// Package shenronpackage validates and installs standalone Shenron packages.
package shenronpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
	"gopkg.in/yaml.v3"
)

const (
	// ManifestFileName is the required package manifest at the package root.
	ManifestFileName = "shenron-package.yaml"
	// PivotFileName is the standalone Shenron pivot at the package root.
	PivotFileName = "shenron.yaml"

	indexFileName     = "index.json"
	indexLockFileName = "index.lock"
)

var (
	kebabCase = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	semver    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

// Manifest describes a standalone, shareable Shenron configuration package.
type Manifest struct {
	SchemaVersion string            `yaml:"schemaVersion"`
	Name          string            `yaml:"name"`
	Version       string            `yaml:"version"`
	Description   string            `yaml:"description"`
	Skills        SkillRequirements `yaml:"skills,omitempty"`
}

// SkillRequirements declares the skills a package expects to be present.
type SkillRequirements struct {
	Required []string `yaml:"required,omitempty"`
	Optional []string `yaml:"optional,omitempty"`
}

// Package is a validated package directory.
type Package struct {
	Root     string
	Manifest Manifest
	Pivot    *pivot.PivotFile
}

// InstalledPackage is the durable record of one installed local package.
type InstalledPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Root        string `json:"root"`
	Digest      string `json:"digest"`
}

// Store owns an injectable package-cache root. It is safe to use in tests
// without relying on a user's home directory.
type Store struct {
	root string
}

// NewStore creates a package store rooted at root.
func NewStore(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

// LoadDirectory parses and validates a standalone package directory.
func LoadDirectory(root string) (*Package, error) {
	realRoot, err := resolveDirectory(root)
	if err != nil {
		return nil, err
	}
	if err := validateNoSymlinks(realRoot); err != nil {
		return nil, err
	}

	manifestData, err := os.ReadFile(filepath.Join(realRoot, ManifestFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ManifestFileName, err)
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return nil, err
	}

	pivotData, err := os.ReadFile(filepath.Join(realRoot, PivotFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", PivotFileName, err)
	}
	pf, err := pivot.Parse(pivotData, realRoot)
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", PivotFileName, err)
	}
	if err := validatePromptContainment(realRoot, pf); err != nil {
		return nil, err
	}

	return &Package{Root: realRoot, Manifest: *manifest, Pivot: pf}, nil
}

func resolveDirectory(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve package root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve package root: %w", err)
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return "", fmt.Errorf("stat package root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("package root is not a directory: %s", root)
	}
	return realRoot, nil
}

// ParseManifest decodes a strict manifest and validates its structural and
// cross-field invariants.
func ParseManifest(data []byte) (*Manifest, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ManifestFileName, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", ManifestFileName, err)
		}
		return nil, fmt.Errorf("parse %s: multiple YAML documents are not supported", ManifestFileName)
	}
	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func validateNoSymlinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fmt.Errorf("package contains symlink %q; symlinks are not supported", rel)
	})
}

func validateManifest(manifest *Manifest) error {
	var errs []string
	if manifest.SchemaVersion != "1" {
		errs = append(errs, `schemaVersion must be "1"`)
	}
	if !kebabCase.MatchString(manifest.Name) {
		errs = append(errs, "name must match ^[a-z][a-z0-9-]*$")
	}
	if !semver.MatchString(manifest.Version) {
		errs = append(errs, "version must be a semantic version")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		errs = append(errs, "description is required")
	}
	errSkills := validateSkills(manifest.Skills)
	errs = append(errs, errSkills...)
	if len(errs) > 0 {
		return fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateSkills(skills SkillRequirements) []string {
	var errs []string
	required := make(map[string]struct{}, len(skills.Required))
	for i, skill := range skills.Required {
		if !kebabCase.MatchString(skill) {
			errs = append(errs, fmt.Sprintf("skills.required[%d] must match ^[a-z][a-z0-9-]*$", i))
		}
		if _, exists := required[skill]; exists {
			errs = append(errs, fmt.Sprintf("skills.required[%d] duplicates %q", i, skill))
		}
		required[skill] = struct{}{}
	}
	seenOptional := make(map[string]struct{}, len(skills.Optional))
	for i, skill := range skills.Optional {
		if !kebabCase.MatchString(skill) {
			errs = append(errs, fmt.Sprintf("skills.optional[%d] must match ^[a-z][a-z0-9-]*$", i))
		}
		if _, exists := seenOptional[skill]; exists {
			errs = append(errs, fmt.Sprintf("skills.optional[%d] duplicates %q", i, skill))
		}
		seenOptional[skill] = struct{}{}
		if _, exists := required[skill]; exists {
			errs = append(errs, fmt.Sprintf("skills.required and skills.optional overlap on %q", skill))
		}
	}
	return errs
}

func validatePromptContainment(root string, pf *pivot.PivotFile) error {
	for i, agent := range pf.Agents {
		if strings.TrimSpace(agent.PromptFile) == "" {
			continue
		}
		candidate := filepath.Clean(filepath.Join(root, agent.PromptFile))
		if !isWithin(root, candidate) {
			return fmt.Errorf("agents[%d].promptFile escapes package root: %s", i, agent.PromptFile)
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("agents[%d].promptFile resolve: %w", i, err)
		}
		if !isWithin(root, resolved) {
			return fmt.Errorf("agents[%d].promptFile escapes package root through symlink: %s", i, agent.PromptFile)
		}
	}
	return nil
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// InstallLocal copies the current source into a content-addressed cache
// snapshot, then validates and identifies that immutable snapshot. A package
// name can be installed only once until update semantics are introduced by the
// CLI layer.
func (s *Store) InstallLocal(source string) (*InstalledPackage, error) {
	unlock, err := s.lockIndex()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unlock() }()

	staged, err := s.stageLocalSnapshot(source)
	if err != nil {
		return nil, err
	}
	defer staged.cleanup()

	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	for _, installed := range index.Packages {
		if installed.Name == staged.pkg.Manifest.Name {
			return nil, fmt.Errorf("package %q is already installed", staged.pkg.Manifest.Name)
		}
	}

	target := filepath.Join(s.root, "packages", staged.pkg.Manifest.Name, staged.digest)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("create package snapshot parent: %w", err)
	}
	if _, err := os.Stat(target); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat package snapshot: %w", err)
	} else if os.IsNotExist(err) {
		if err := os.Rename(staged.root, target); err != nil {
			return nil, fmt.Errorf("publish package snapshot: %w", err)
		}
	}
	if err := validateSnapshotDigest(target, staged.digest); err != nil {
		return nil, err
	}

	installed := InstalledPackage{
		Name: staged.pkg.Manifest.Name, Version: staged.pkg.Manifest.Version, Description: staged.pkg.Manifest.Description,
		Source: staged.source, Root: target, Digest: staged.digest,
	}
	index.Packages = append(index.Packages, installed)
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].Name < index.Packages[j].Name })
	if err := s.writeIndex(index); err != nil {
		return nil, err
	}
	return &installed, nil
}

type stagedSnapshot struct {
	source string
	root   string
	tmp    string
	pkg    *Package
	digest string
}

func (s *Store) stageLocalSnapshot(source string) (*stagedSnapshot, error) {
	sourceRoot, err := resolveDirectory(source)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(s.root, "packages")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create package snapshot parent: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".package-")
	if err != nil {
		return nil, fmt.Errorf("create package snapshot: %w", err)
	}
	root := filepath.Join(tmp, "contents")
	if err := copyDirectory(sourceRoot, root); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("copy package snapshot: %w", err)
	}
	pkg, err := LoadDirectory(root)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("validate package snapshot: %w", err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("hash package snapshot: %w", err)
	}
	return &stagedSnapshot{source: sourceRoot, root: root, tmp: tmp, pkg: pkg, digest: digest}, nil
}

func (s *stagedSnapshot) cleanup() {
	_ = os.RemoveAll(s.tmp)
}

// List returns installed packages ordered by name.
func (s *Store) List() ([]InstalledPackage, error) {
	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	return append([]InstalledPackage(nil), index.Packages...), nil
}

type packageIndex struct {
	Version  string             `json:"version"`
	Packages []InstalledPackage `json:"packages"`
}

func (s *Store) indexPath() string { return filepath.Join(s.root, indexFileName) }

func (s *Store) lockIndex() (func() error, error) {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("create package cache: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.root, indexLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open package index lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock package index: %w", err)
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return fmt.Errorf("unlock package index: %w", unlockErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close package index lock: %w", closeErr)
		}
		return nil
	}, nil
}

func (s *Store) readIndex() (*packageIndex, error) {
	data, err := os.ReadFile(s.indexPath())
	if os.IsNotExist(err) {
		return &packageIndex{Version: "1"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read package index: %w", err)
	}
	var index packageIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse package index: %w", err)
	}
	if index.Version != "1" {
		return nil, fmt.Errorf("unsupported package index version %q", index.Version)
	}
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].Name < index.Packages[j].Name })
	return &index, nil
}

func (s *Store) writeIndex(index *packageIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode package index: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(s.indexPath(), data, 0o644); err != nil {
		return fmt.Errorf("write package index: %w", err)
	}
	return nil
}

func directoryDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(hash, rel+"\x00"); err != nil {
			return err
		}
		if entry.IsDir() {
			_, err := io.WriteString(hash, "dir\x00")
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, err = io.WriteString(hash, "link\x00"+link+"\x00")
			return err
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", rel)
		}
		if _, err := io.WriteString(hash, "file\x00"); err != nil {
			return err
		}
		file, err := openRegularFileNoFollow(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateSnapshotDigest(root, expectedDigest string) error {
	pkg, err := LoadDirectory(root)
	if err != nil {
		return fmt.Errorf("validate package snapshot: %w", err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		return fmt.Errorf("hash package snapshot: %w", err)
	}
	if digest != expectedDigest {
		return fmt.Errorf("package snapshot digest mismatch: got %s, want %s", digest, expectedDigest)
	}
	return nil
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := destination
		if rel != "." {
			target = filepath.Join(destination, rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in package snapshots: %s", rel)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", rel)
		}
		input, err := openRegularFileNoFollow(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutErr := output.Close()
		closeInErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		return closeInErr
	})
}

func openRegularFileNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("not a regular file")
	}
	return os.NewFile(uintptr(fd), path), nil
}
