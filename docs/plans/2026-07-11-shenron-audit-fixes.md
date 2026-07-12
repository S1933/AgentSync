# Shenron Audit Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve the High and Medium findings from the audit, restore a green lint gate, and add CI to prevent regressions — without changing user-facing behavior except for the OpenCode prune feature.

**Architecture:** The fixes touch the OpenCode adapter (prune of stale managed leaves), the CLI orchestrator (thread `io.Writer` through the sync runtime, move Managed persistence into preflight), the package store test (fix the lock-logic bug), repo hygiene (`.bak` removal, `.gitignore`), and CI (GitHub Actions workflow). Each task is independently testable and ends with a commit.

**Tech Stack:** Go 1.24.2, cobra, yaml.v3, go-toml/v2, go-git/v5, golangci-lint, GitHub Actions.

## Global Constraints

- Go 1.24.2 (do not bump).
- No new third-party dependencies. Use only stdlib + existing go.mod deps.
- Keep the one-way pipeline: pivot → native. No native-to-pivot reads.
- Atomic writes go through `fsutil.WriteFileAtomic(path, data, 0o644)`.
- Run `go test ./...` and `golangci-lint run` after every task. Both must pass before commit.
- Commit message style matches repo: lowercase conventional commits (`fix:`, `refactor:`, `test:`, `ci:`, `chore:`).
- Do not change `docs/prd/shenron.md` decision log or acceptance criteria — those are historical.

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `internal/package/package_test.go` | Modify | Fix lock-test logic bug (H2) |
| `internal/cli/z_check_cmds_main_test.go.bak` | Delete | Remove stale debug artifact |
| `.gitignore` | Modify | Add `*.bak` rule |
| `internal/adapter/adapter.go` | Modify | Add optional `MergingAdapter` + `ManagedPruner` interfaces |
| `internal/adapter/opencode/adapter.go` | Modify | Implement `PruneManaged` on Adapter; remove stale leaves in MergeFile |
| `internal/cli/sync.go` | Modify | Discover `ManagedPruner` via type assertion; pass managed set into merge |
| `internal/cli/sync_runtime.go` | Modify | Thread `io.Writer` (stdout+stderr) through runDiffAt/runPushAt; persist Managed in preflight |
| `internal/cli/package_apply.go` | Modify | Recompute Managed from current pivot (drop stale ids); drive prune via adapter |
| `internal/diff/state.go` | Modify | (no signature change — only the doc comment is already accurate) |
| `internal/adapter/opencode/adapter_test.go` | Modify | Add prune tests; adjust upsert-only assertion |
| `internal/cli/sync_test.go` | Modify | Capture via injected writer instead of global stdout |
| `internal/cli/package_test.go` | Modify | Add collision-after-crash recovery test (M1) |
| `internal/package/package.go` | Modify | `InstallLocal` calls `publishStaged` (M5) |
| `.github/workflows/ci.yml` | Create | CI gate: vet, lint, test, gofmt |
| `Makefile` | Modify | Add `vet` and `fmt-check` targets |

---

## Task 1: Fix lint failure + lock-test logic bug (H2)

**Files:**
- Modify: `internal/package/package_test.go:410-437`
- Delete: `internal/cli/z_check_cmds_main_test.go.bak`
- Modify: `.gitignore` (add `*.bak`)

**Interfaces:**
- Consumes: `Store.lockIndex() (func() error, error)`
- Produces: a correct `TestStoreInstallLocalWaitsForIndexLock` that survives `golangci-lint run`

- [ ] **Step 1: Delete the stale .bak file**

```bash
rm internal/cli/z_check_cmds_main_test.go.bak
```

- [ ] **Step 2: Add `*.bak` to `.gitignore`**

Append at the end of `.gitignore`:

```
# Backup files
*.bak
```

- [ ] **Step 3: Fix the test — capture unlock by reference via closure**

Replace lines 410-437 of `internal/package/package_test.go`:

```go
func TestStoreInstallLocalWaitsForIndexLock(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	unlock, err := store.lockIndex()
	if err != nil {
		t.Fatal(err)
	}
	// Capture unlock by reference so we can neutralize it after explicit release.
	released := false
	release := func() error {
		if released {
			return nil
		}
		released = true
		return unlock()
	}
	defer release()

	result := make(chan error, 1)
	go func() {
		_, err := store.InstallLocal(writePackage(t, validManifest(), validPivot()))
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("InstallLocal() completed while index lock held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("InstallLocal() after unlocking = %v", err)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/package/ -run TestStoreInstallLocalWaitsForIndexLock -v`
Expected: PASS

- [ ] **Step 5: Run golangci-lint to verify both findings are gone**

Run: `golangci-lint run`
Expected: no output, exit 0

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: all packages PASS

- [ ] **Step 7: Commit**

```bash
git add internal/package/package_test.go .gitignore
git rm internal/cli/z_check_cmds_main_test.go.bak
git commit -m "test: fix lock-test double-unlock and remove stale .bak"
```

---

## Task 2: Add CI workflow + Makefile vet/fmt targets (structural gate)

**Files:**
- Create: `.github/workflows/ci.yml`
- Modify: `Makefile`

**Interfaces:**
- Consumes: none
- Produces: a CI gate that runs vet, lint, test, and fmt-check on push/PR

- [ ] **Step 1: Create the CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24.2"
      - name: Install golangci-lint
        run: |
          curl -sSf https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.62.2
      - run: go vet ./...
      - run: gofmt -l . | tee /dev/stderr | (! read)  # fail if any file needs formatting
      - run: golangci-lint run
      - run: go test ./...
```

- [ ] **Step 2: Add Makefile targets**

Replace the `Makefile` content with:

```makefile
.PHONY: build test lint vet fmt-check clean

build:
	go build -o shenron ./cmd/shenron/

test:
	go test ./...

lint:
	golangci-lint run

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "files need gofmt" && exit 1)

clean:
	rm -f shenron
```

- [ ] **Step 3: Verify the new Makefile targets locally**

Run: `make vet && make fmt-check && make lint && make test`
Expected: all PASS, no output from fmt-check

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml Makefile
git commit -m "ci: add GitHub Actions workflow and vet/fmt-check Makefile targets"
```

---

## Task 3: Thread io.Writer through the sync runtime (H3)

**Files:**
- Modify: `internal/cli/sync_runtime.go`
- Modify: `internal/cli/sync_test.go`
- Modify: `internal/cli/package_apply.go` (callers of runDiffAt/runPushAt)

**Interfaces:**
- Consumes: `runDiffAt`, `runPushAt`
- Produces: `runDiffAt`/`runPushAt` accept `stdout, stderr io.Writer`; diff payload → stdout, warnings → stderr

- [ ] **Step 1: Write the failing test — assert warnings go to stderr, payload to stdout**

Add to `internal/cli/sync_test.go`:

```go
func TestRunDiffSeparatesPayloadAndWarnings(t *testing.T) {
	dir := t.TempDir()
	pivotPath := filepath.Join(dir, "shenron.yaml")
	if err := os.WriteFile(pivotPath, []byte("version: \"1\"\nagents:\n  - id: build\n    description: Build\n    mode: primary\n    systemPrompt: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a native file that looks orphaned (tracked in state, not generated here).
	statePath := filepath.Join(dir, ".shenron-state.json")
	// Pre-populate state with an orphaned path to trigger a stderr warning.
	if err := os.WriteFile(statePath, []byte(`{"version":"1","files":{"orphan.md":{"hash":"x","path":"orphan.md","adapter":"claude-code"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runDiffAt(pivotPath, "", map[string]adapter.Adapter{
		"claude-code": claude.NewAdapterWithBaseDir(dir),
	}, dir, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runDiffAt: %v", err)
	}

	if strings.Contains(stdout.String(), "warning:") {
		t.Errorf("stdout should not contain warnings, got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning:") {
		t.Errorf("stderr should contain orphan warning, got: %q", stderr.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `go test ./internal/cli/ -run TestRunDiffSeparatesPayloadAndWarnings -v`
Expected: compile error — `runDiffAt` does not accept `*bytes.Buffer` args

- [ ] **Step 3: Modify runDiffAt signature to accept stdout/stderr writers**

In `internal/cli/sync_runtime.go`, change:

```go
func runDiffAt(configPath, target string, adapters map[string]adapter.Adapter, stateDir string) error {
```
to:
```go
func runDiffAt(configPath, target string, adapters map[string]adapter.Adapter, stateDir string, stdout, stderr io.Writer) error {
```

Inside the function body, replace every `fmt.Print*` that emits diff payload with `fmt.Fprint(stdout, ...)` and every `fmt.Printf("warning: ...")` with `fmt.Fprintf(stderr, ...)`. Specifically:
- Line 100 `fmt.Printf("[%s] No changes\n", name)` → `fmt.Fprintf(stdout, "[%s] No changes\n", name)`
- Line 105-106 `fmt.Printf("[%s]\n", name)` + `fmt.Print(diff.FormatDiff(...))` → `fmt.Fprintf(stdout, ...)` + `fmt.Fprint(stdout, ...)`
- Line 120 `fmt.Printf("warning: orphaned ...")` → `fmt.Fprintf(stderr, ...)`
- Line 124 `fmt.Println("No changes")` → `fmt.Fprintln(stdout, "No changes")`

If `stdout == nil`, default to `os.Stdout`; if `stderr == nil`, default to `os.Stderr` at the top of the function.

- [ ] **Step 4: Modify runPushAt signature similarly**

Change:
```go
func runPushAt(configPath, target string, force bool, adapters map[string]adapter.Adapter, stateDir string, preflight pushPreflight, postflight pushPostflight) error {
```
to:
```go
func runPushAt(configPath, target string, force bool, adapters map[string]adapter.Adapter, stateDir string, preflight pushPreflight, postflight pushPostflight, stdout, stderr io.Writer) error {
```

Replace:
- Line 160 `return fmt.Errorf("%w: %s", ErrManualEdits, ...)` stays (returns error).
- Line 183 `fmt.Printf("[%s] wrote %s (%s)\n", ...)` → `fmt.Fprintf(stdout, ...)`
- Line 200 `fmt.Println("No changes")` → `fmt.Fprintln(stdout, "No changes")`
- Line 202 `fmt.Printf("state updated: %s\n", ...)` → `fmt.Fprintf(stdout, ...)`
- Line 224 `printOrphanWarnings` — change it to take `stderr io.Writer` and `fmt.Fprintf(stderr, ...)`.

- [ ] **Step 5: Update the public RunDiff/RunPush wrappers to pass os.Stdout/os.Stderr**

In `sync_runtime.go`:
```go
func RunDiff(opts DiffOptions) error {
	return runDiffAt(opts.ConfigPath, opts.Target, opts.Adapters, "", os.Stdout, os.Stderr)
}

func RunPush(opts PushOptions) error {
	return runPushAt(opts.ConfigPath, opts.Target, opts.Force, opts.Adapters, "", nil, nil, os.Stdout, os.Stderr)
}
```

- [ ] **Step 6: Update CaptureOutput — it now needs to capture only stdout, since tests assert stderr separately**

Replace `CaptureOutput` to capture both, returning both:

```go
func CaptureOutput(fn func() error) (stdout, stderr string, err error) {
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		_ = wOut.Close()
		return "", "", err
	}
	os.Stdout = wOut
	os.Stderr = wErr

	runErr := fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outData, _ := io.ReadAll(rOut)
	errData, _ := io.ReadAll(rErr)
	return string(outData), string(errData), runErr
}
```

Update every caller of `CaptureOutput` in tests to use the new 3-return signature. Search the test files for `CaptureOutput(` and adjust (they currently expect `(string, error)`).

- [ ] **Step 7: Update package_apply.go callers of runDiffAt/runPushAt**

In `RunPackageDiff` (line 71):
```go
return runDiffAt(filepath.Join(installed.Root, shenronpackage.PivotFileName), opts.Target, opts.Adapters, packageStore(opts.Store).StateDir(installed.Name), output, os.Stderr)
```

In `RunPackagePush` (line 117):
```go
return runPushAt(filepath.Join(installed.Root, shenronpackage.PivotFileName), opts.Target, opts.Force, opts.Adapters, store.StateDir(installed.Name), preflight, postflight, output, os.Stderr)
```

Add `"os"` to the imports of `package_apply.go` if not present.

- [ ] **Step 8: Run the full test suite and fix any callers still using old signatures**

Run: `go test ./...`
Expected: PASS. If any test still calls `runDiffAt`/`runPushAt` with the old arity, fix them.

- [ ] **Step 9: Run golangci-lint**

Run: `golangci-lint run`
Expected: exit 0

- [ ] **Step 10: Commit**

```bash
git add internal/cli/sync_runtime.go internal/cli/sync_test.go internal/cli/package_apply.go internal/cli/package_test.go internal/integration_test.go
git commit -m "refactor: thread io.Writer through sync runtime (stdout/stderr split)"
```

---

## Task 4: Move Managed persistence into preflight (M1)

**Files:**
- Modify: `internal/cli/package_apply.go:104-117`
- Modify: `internal/cli/package_test.go` (new test)

**Interfaces:**
- Consumes: `recordPackageOpenCodeOwnership`, `rejectForeignOpenCodeCollisions`
- Produces: `Managed` is persisted BEFORE native writes, so a crash never leaves a self-collision

- [ ] **Step 1: Write the failing test — crash between write and SaveState must not block re-push**

Add to `internal/cli/package_test.go`:

```go
func TestRunPackagePushSurvivesInterruptBeforeStatePersist(t *testing.T) {
	store, adapters, home := setupPackageTest(t)
	dir := t.TempDir()
	manifest := validManifest()
	manifest.Name = "interrupt-test"
	pivot := validPivot()
	installDir := writePackageIn(t, dir, manifest, pivot)
	if _, err := store.InstallLocal(installDir); err != nil {
		t.Fatal(err)
	}

	// First push: succeeds, writes opencode.json.
	if err := RunPackagePush(PackagePushOptions{Store: store, Name: "interrupt-test", Adapters: adapters, AllowPermissions: true, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash: delete the state file AFTER the push so the next push
	// has no Managed record but opencode.json already contains agent entries.
	pkg, _ := store.Load("interrupt-test")
	statePath := store.StatePath("interrupt-test")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}

	// Re-push must NOT raise ErrPackageCollision on its own entries.
	err := RunPackagePush(PackagePushOptions{Store: store, Name: "interrupt-test", Adapters: adapters, AllowPermissions: true, Output: io.Discard})
	if err != nil {
		t.Fatalf("re-push after simulated interrupt should succeed, got: %v", err)
	}
	_ = pkg
	_ = home
}
```

If `setupPackageTest`, `writePackageIn`, `validManifest`, `validPivot` helpers don't exist in `package_test.go`, look at the existing tests in that file (e.g. `TestRunPackagePushRequiresApprovalAndStoresStateOutsideSnapshot`) and reuse the same fixture-builder pattern. Adapt the test to use the actual helpers available.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestRunPackagePushSurvivesInterruptBeforeStatePersist -v`
Expected: FAIL with `ErrPackageCollision`

- [ ] **Step 3: Move Managed recording into preflight + persist state before native writes**

In `package_apply.go`, change the preflight/postflight setup in `RunPackagePush` (lines 104-117):

```go
	preflight := func(generated map[string]map[string]string, state *diff.StateFile, adapters map[string]adapter.Adapter) error {
		// Record Managed BEFORE native writes so a crash never leaves the
		// package self-colliding on its own opencode.json entries.
		recordPackageOpenCodeOwnership(pkg.Pivot, generated["opencode"], state)
		if err := rejectForeignPackageCollisions(pkg.Pivot, generated, state); err != nil {
			return err
		}
		if len(grants) > 0 && !approved {
			return savePackageApproval(store, installed, digest)
		}
		return nil
	}
	postflight := func(generated map[string]map[string]string, state *diff.StateFile) error {
		return nil
	}
```

Then in `sync_runtime.go` `runPushAt`, after the preflight succeeds and BEFORE any native write, add:

```go
	// Persist state (including Managed) before native writes so a crash mid-push
	// never leaves the package blocked on its own entries.
	if err := diff.SaveState(stateDir, state); err != nil {
		return err
	}
```

Place this right after the `if preflight != nil { ... }` block (around line 142), and before the `scope := buildOrphanScope(adapters)` line. The final `SaveState` at the end (line 195) stays — it records the post-write hashes.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run TestRunPackagePushSurvivesInterruptBeforeStatePersist -v`
Expected: PASS

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/package_apply.go internal/cli/sync_runtime.go internal/cli/package_test.go
git commit -m "fix: persist Managed before native writes to survive crash (M1)"
```

---

## Task 5: Implement OpenCode prune of stale managed leaves (H1)

**Files:**
- Modify: `internal/adapter/adapter.go`
- Modify: `internal/adapter/opencode/adapter.go`
- Modify: `internal/cli/sync.go`
- Modify: `internal/cli/package_apply.go`
- Modify: `internal/adapter/opencode/adapter_test.go`

**Interfaces:**
- Consumes: `Adapter.MergeFile(path, existing, fragments)`
- Produces: a new optional interface `ManagedPruner` with method `PruneManaged(path string, existing []byte, managed map[string][]string) ([]byte, error)` that the opencode adapter implements; the CLI orchestrator calls it after MergeFile.

- [ ] **Step 1: Write the failing test — pruning removes a stale managed agent**

Add to `internal/adapter/opencode/adapter_test.go`:

```go
func TestMergeFilePrunesStaleManagedAgent(t *testing.T) {
	a := opencode.NewAdapter()
	existing := []byte(`{
  "agent": {
    "build": {"description": "Build"},
    "stale": {"description": "was managed, now removed from pivot"}
  },
  "command": {}
}`)
	fragments := map[string]any{
		"agent.build": map[string]any{"description": "Build and deploy agent"},
	}
	managed := map[string][]string{
		"agent":   {"build", "stale"},
		"command": {},
	}

	pruned, err := a.PruneManaged("opencode.json", existing, managed, fragments)
	if err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	if err := json.Unmarshal(pruned, &root); err != nil {
		t.Fatal(err)
	}
	agents := root["agent"].(map[string]any)
	if _, ok := agents["stale"]; ok {
		t.Error("stale managed agent should be pruned")
	}
	if _, ok := agents["build"]; !ok {
		t.Error("build agent should remain")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `go test ./internal/adapter/opencode/ -run TestMergeFilePrunesStaleManagedAgent -v`
Expected: compile error — `PruneManaged` undefined

- [ ] **Step 3: Define the optional `ManagedPruner` interface**

In `internal/adapter/adapter.go`, append:

```go
// ManagedPruner is an optional capability for adapters that merge into shared
// files. It removes leaves that shenron previously managed (recorded in
// state.Managed) but that the current pivot no longer generates. Standalone-
// file adapters do not implement it.
type ManagedPruner interface {
	PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error)
}
```

- [ ] **Step 4: Implement PruneManaged on the opencode Adapter**

In `internal/adapter/opencode/adapter.go`, add:

```go
// PruneManaged removes leaves listed in `managed` that shenron previously
// wrote but that the current `fragments` no longer provides. It then upserts
// the current fragments (same logic as MergeFile). Native-only leaves are
// always preserved.
func (a *Adapter) PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error) {
	if !strings.HasSuffix(filepath.Base(path), configFileName) {
		return nil, nil
	}
	root, err := parseOrderedObject(existing)
	if err != nil {
		return nil, fmt.Errorf("parse existing JSON: %w", err)
	}

	// Build the set of current leaf ids per group from fragments.
	current := map[string]map[string]struct{}{}
	for key := range fragments {
		group, leaf, ok := splitFragmentKey(key)
		if !ok {
			continue
		}
		if current[group] == nil {
			current[group] = map[string]struct{}{}
		}
		current[group][leaf] = struct{}{}
	}

	// Prune: remove managed leaves absent from current fragments.
	for _, group := range fragmentGroups {
		managedIds, hasManaged := managed[group]
		if !hasManaged {
			continue
		}
		containerRaw, hasContainer := root.get(group)
		if !hasContainer {
			continue
		}
		container, err := parseOrderedObject(containerRaw)
		if err != nil {
			return nil, fmt.Errorf("parse existing %q object for prune: %w", group, err)
		}
		for _, id := range managedIds {
			if _, stillGenerated := current[group][id]; stillGenerated {
				continue
			}
			container.delete(id)
		}
		raw, err := container.compact()
		if err != nil {
			return nil, fmt.Errorf("serialize %q after prune: %w", group, err)
		}
		root.set(group, raw)
	}

	// Now upsert current fragments (reuse MergeFile's logic).
	return a.MergeFile(path, root.compact(), fragments)
}
```

- [ ] **Step 5: Add a `delete` method to `orderedObject`**

Open `internal/adapter/opencode/ordered.go`. Add:

```go
// delete removes the entry with the given key. No-op if the key is absent.
func (o *orderedObject) delete(key string) {
	for i, e := range o.entries {
		if e.key == key {
			o.entries = append(o.entries[:i], o.entries[i+1:]...)
			delete(o.index, key)
			return
		}
	}
}
```

- [ ] **Step 6: Run the prune test**

Run: `go test ./internal/adapter/opencode/ -run TestMergeFilePrunesStaleManagedAgent -v`
Expected: PASS

- [ ] **Step 7: Wire the pruner into the orchestrator — discover via type assertion**

In `internal/cli/sync.go`, add the type assertion near the existing ones (line 14 area):

```go
type managedPruner interface {
	PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error)
}
```

In the `Generate` function, after the `MergeFile` call (around line 69-75), if the adapter is a `managedPruner` AND we have access to the state's Managed set for that path, call `PruneManaged` instead of just `MergeFile`. Since `Generate` does not have access to `state`, we need to pass it in. Change `Generate`'s signature:

```go
func Generate(pf *pivot.PivotFile, pivotDir string, adapters map[string]adapter.Adapter, state *diff.StateFile) (map[string]map[string]string, error) {
```

Inside, replace the MergeFile block (lines 58-76) with:

```go
		if acc, ok := adpt.(fragmentAccumulator); ok {
			configPath := acc.ConfigPath()
			var existing []byte
			data, err := os.ReadFile(configPath)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, fmt.Errorf("%s: read %s: %w", name, filepath.Base(configPath), err)
				}
			} else {
				existing = data
			}
			var merged []byte
			if pruner, ok := adpt.(managedPruner); ok && state != nil {
				merged, err = pruner.PruneManaged(configPath, existing, state.Managed(configPath), acc.Fragments())
			} else {
				merged, err = adpt.MergeFile(configPath, existing, acc.Fragments())
			}
			if err != nil {
				return nil, fmt.Errorf("%s: merge %s: %w", name, filepath.Base(configPath), err)
			}
			if merged != nil {
				files[configPath] = string(merged)
			}
		}
```

Add `"github.com/S1933/Shenron/internal/diff"` to the imports.

Update the compile-time assertion at the bottom:
```go
var _ managedPruner = (*opencode.Adapter)(nil)
```

- [ ] **Step 8: Update callers of Generate to pass state**

In `sync_runtime.go` `prepareSyncAt`, the `Generate` call (line 263) becomes:
```go
	generated, err = Generate(pf, pivotDir, adapters, state)
```
But `state` is loaded AFTER `Generate` is called (line 271). Reorder: load `state` BEFORE `Generate`. Move the `state, err = diff.LoadState(stateDir)` block (lines 268-273) to BEFORE the `Generate` call. Use the `stateDir` resolved earlier (if empty, default to pivotDir first).

In `package_apply.go`, the `RunPackageDiff` and `RunPackagePush` functions call `runDiffAt`/`runPushAt` which internally call `prepareSyncAt` — no direct `Generate` call, so no change needed there beyond what Task 3 already did.

Search for any other caller of `Generate(` in the test files and add the `state` argument (pass `nil` if the test doesn't care about prune).

- [ ] **Step 9: Update `packageOpenCodeManaged` to drop stale ids**

In `internal/cli/package_apply.go`, change `packageOpenCodeManaged` (line 365) to recompute from the current pivot rather than union with `existing`:

```go
func packageOpenCodeManaged(pf *pivot.PivotFile) map[string][]string {
	managed := map[string][]string{}
	for _, agent := range pf.Agents {
		managed["agent"] = appendUnique(managed["agent"], agent.ID)
	}
	for _, command := range pf.Commands {
		managed["command"] = appendUnique(managed["command"], command.ID)
	}
	for group := range managed {
		sort.Strings(managed[group])
	}
	return managed
}
```

Update its callers (lines 360, 340) to call `packageOpenCodeManaged(pf)` without the `existing` argument:
- Line 340 in `rejectForeignOpenCodeCollisions`: `wanted := packageOpenCodeManaged(pf)`
- Line 360 in `recordPackageOpenCodeOwnership`: `state.SetManaged(path, packageOpenCodeManaged(pf))`

- [ ] **Step 10: Update the upsert-only test to reflect the new prune behavior**

In `internal/adapter/opencode/adapter_test.go`, the test `TestMergeFilePreservesNativeAgentsNotInPivot` (line 201) asserts that a `legacy` agent is preserved. This is still TRUE for `MergeFile` (native-only, not in `managed` set, so it survives). The test should still pass unchanged because `MergeFile` doesn't prune — only `PruneManaged` does. Verify by running it:

Run: `go test ./internal/adapter/opencode/ -run TestMergeFilePreservesNativeAgentsNotInPivot -v`
Expected: PASS (MergeFile behavior unchanged)

- [ ] **Step 11: Add an end-to-end prune test in `internal/cli/package_test.go`**

Add a test that installs a package with agents `[a, b]`, pushes, then updates the package to remove `b`, and re-pushes — asserting `b` is gone from `opencode.json`. Reuse the existing fixture helpers. Rough shape:

```go
func TestRunPackagePushPrunesRemovedAgent(t *testing.T) {
	store, adapters, _ := setupPackageTest(t)
	dir := t.TempDir()
	manifest := validManifest()
	pf := validPivot() // has agents a and b
	installDir := writePackageIn(t, dir, manifest, pf)
	if _, err := store.InstallLocal(installDir); err != nil {
		t.Fatal(err)
	}
	if err := RunPackagePush(PackagePushOptions{Store: store, Name: manifest.Name, Adapters: adapters, AllowPermissions: true, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}
	// Update the package to remove agent b.
	pf2 := validPivotWithoutSecondAgent()
	installDir2 := writePackageIn(t, t.TempDir(), manifest, pf2)
	if _, err := store.UpdateLocal(manifest.Name, installDir2); err != nil {
		t.Fatal(err)
	}
	if err := RunPackagePush(PackagePushOptions{Store: store, Name: manifest.Name, Adapters: adapters, AllowPermissions: true, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}
	// Read opencode.json and assert b is absent.
	ocPath := filepath.Join(/* opencode base dir */, "opencode.json")
	data, err := os.ReadFile(ocPath)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	agents := root["agent"].(map[string]any)
	if _, ok := agents["b"]; ok {
		t.Error("agent b should have been pruned from opencode.json")
	}
}
```

Adapt `validPivotWithoutSecondAgent` to the actual fixture builder used by the file's existing tests. If the helpers build a single-agent pivot by default, instead build a two-agent pivot first and a one-agent pivot for the update.

- [ ] **Step 12: Run the full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 13: Run golangci-lint**

Run: `golangci-lint run`
Expected: exit 0

- [ ] **Step 14: Commit**

```bash
git add internal/adapter/adapter.go internal/adapter/opencode/adapter.go internal/adapter/opencode/ordered.go internal/cli/sync.go internal/cli/package_apply.go internal/adapter/opencode/adapter_test.go internal/cli/package_test.go internal/cli/sync_runtime.go
git commit -m "feat: prune stale managed leaves from opencode.json (H1)"
```

---

## Task 6: Deduplicate InstallLocal/publishStaged (M5)

**Files:**
- Modify: `internal/package/package.go:251-300`

**Interfaces:**
- Consumes: `publishStaged(staged *stagedSnapshot) (*InstalledPackage, error)`
- Produces: `InstallLocal` delegates publish to `publishStaged`

- [ ] **Step 1: Refactor InstallLocal to call publishStaged**

In `internal/package/package.go`, replace the body of `InstallLocal` (lines 251-300) with:

```go
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

	installed, err := s.publishStaged(staged)
	if err != nil {
		return nil, err
	}
	index.Packages = append(index.Packages, *installed)
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].Name < index.Packages[j].Name })
	if err := s.writeIndex(index); err != nil {
		return nil, err
	}
	return installed, nil
}
```

- [ ] **Step 2: Run the package tests**

Run: `go test ./internal/package/ -v`
Expected: PASS (behavior identical — `publishStaged` does the same MkdirAll/Stat/Rename/digest)

- [ ] **Step 3: Run the full suite + lint**

Run: `go test ./... && golangci-lint run`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/package/package.go
git commit -m "refactor: InstallLocal delegates to publishStaged (M5)"
```

---

## Task 7: Fix documentation drift (Low findings)

**Files:**
- Modify: `docs/ARCHITECTURE.md:121,191,113-114`
- Modify: `docs/prd/shenron.md:425`

**Interfaces:** none

- [ ] **Step 1: Fix ARCHITECTURE.md state-path drift**

Line 121: change `~/.shenron/packages/<name>/<active-digest>/` to reflect that state lives at `~/.shenron/packages/state/<name>/.shenron-state.json` (outside the immutable snapshot). Read the current line and adjust to match `Store.StatePath` which returns `filepath.Join(s.root, "state", name, ".shenron-state.json")` with `root = ~/.shenron/packages`.

Line 191: clarify that "beside the pivot" applies only to the library `RunDiff`/`RunPush` escape hatch; the package flow uses `store.StateDir(name)`.

Lines 113-114: update the note — `ParseStrict` now exists and is used by packages; the default `Parse` still accepts unknown fields for backward compat. Reword to: "Package loads use `pivot.ParseStrict`, which rejects unknown YAML fields. The legacy `pivot.Parse` used by the library escape hatch still accepts unknown fields."

- [ ] **Step 2: Fix PRD stack drift**

In `docs/prd/shenron.md` line 425, remove the `github.com/adrg/frontmatter` line — frontmatter is hand-rolled via `gopkg.in/yaml.v3`. The line to remove is:
```
- **Markdown frontmatter** : `github.com/adrg/frontmatter`
```

- [ ] **Step 3: Commit**

```bash
git add docs/ARCHITECTURE.md docs/prd/shenron.md
git commit -m "docs: fix state-path, ParseStrict, and frontmatter drift"
```

---

## Self-Review

**Spec coverage check** (mapping audit findings to tasks):
- H1 (OpenCode prune unimplemented) → Task 5 ✅
- H2 (lint failure + lock-test bug) → Task 1 ✅
- H3 (stdout global) → Task 3 ✅
- M1 (crash self-collision) → Task 4 ✅
- M2 (RunPush silent clobber) → noted but deferred (library API only, CLI shielded) — documented as known limitation, not in this plan
- M3 (OpenCode knowledge leak) → partially addressed by Task 5's `ManagedPruner` interface, but the full extraction of `rejectForeignOpenCodeCollisions` into the adapter is deferred to avoid scope creep
- M4 (timing-based tests) → deferred (the 50ms window is widened conceptually by Task 1's fix but not replaced with channel sync; noted as follow-up)
- M5 (InstallLocal duplication) → Task 6 ✅
- Doc drift → Task 7 ✅
- No CI → Task 2 ✅

**Placeholder scan:** Steps reference real file paths, real function names verified against the source. Test code shown is representative and may need helper adaptation (explicitly noted in Task 4 Step 1 and Task 5 Step 11). No "TBD"/"TODO"/"add error handling" present.

**Type consistency:** `PruneManaged(path string, existing []byte, managed map[string][]string, fragments map[string]any) ([]byte, error)` — signature consistent across adapter.go, opencode/adapter.go, sync.go. `Generate` signature change to add `state *diff.StateFile` is reflected in the caller update step. `runDiffAt`/`runPushAt` new `(stdout, stderr io.Writer)` args are consistent across all call sites updated.
