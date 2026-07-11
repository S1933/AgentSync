package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var fileRefPattern = regexp.MustCompile(`^\{file:(.+)\}$`)

// InitOptions configures bootstrap source paths for testing.
type InitOptions struct {
	WorkDir     string
	OpenCodeDir string
	ClaudeDir   string
	CodexDir    string
}

// NewInitCmd creates the init subcommand.
func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a skeleton shenron.yaml from existing native configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunInit(InitOptions{})
		},
	}
}

// RunInit bootstraps shenron.yaml from the first available native config source.
func RunInit(opts InitOptions) error {
	workDir := opts.WorkDir
	if workDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		workDir = cwd
	}

	outputPath := filepath.Join(workDir, "shenron.yaml")
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("shenron.yaml already exists in %s (edit the existing file instead)", workDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check existing pivot file: %w", err)
	}

	pf, source, err := bootstrapPivot(opts)
	if err != nil {
		return err
	}
	if pf == nil {
		return fmt.Errorf("no native config found (checked OpenCode, Claude Code, and Codex)")
	}

	data, err := marshalPivot(pf)
	if err != nil {
		return fmt.Errorf("marshal pivot file: %w", err)
	}

	if err := fsutil.WriteFileAtomic(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write shenron.yaml: %w", err)
	}

	fmt.Printf("Created %s from %s (%d agents, %d commands)\n", outputPath, source, len(pf.Agents), len(pf.Commands))
	return nil
}

func bootstrapPivot(opts InitOptions) (*pivot.PivotFile, string, error) {
	opencodeDir := opts.OpenCodeDir
	if opencodeDir == "" {
		opencodeDir = fsutil.OpenCodePath()
	}
	if pf, err := bootstrapFromOpenCode(opencodeDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: skipping OpenCode bootstrap: %v\n", err)
	} else if pf != nil {
		return pf, "opencode", nil
	}

	claudeDir := opts.ClaudeDir
	if claudeDir == "" {
		claudeDir = fsutil.ClaudePath()
	}
	if pf, err := bootstrapFromClaude(claudeDir); err != nil {
		return nil, "", err
	} else if pf != nil {
		return pf, "claude-code", nil
	}

	codexDir := opts.CodexDir
	if codexDir == "" {
		codexDir = fsutil.CodexPath()
	}
	if pf, err := bootstrapFromCodex(codexDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: skipping Codex bootstrap: %v\n", err)
	} else if pf != nil {
		return pf, "codex", nil
	}

	return nil, "", nil
}

type codexAgentFile struct {
	Name                  string   `toml:"name"`
	Description           string   `toml:"description"`
	DeveloperInstructions string   `toml:"developer_instructions"`
	Model                 string   `toml:"model"`
	ModelReasoningEffort  string   `toml:"model_reasoning_effort"`
	SandboxMode           string   `toml:"sandbox_mode"`
	ApprovalPolicy        string   `toml:"approval_policy"`
	WebSearch             string   `toml:"web_search"`
	NicknameCandidates    []string `toml:"nickname_candidates"`
}

func bootstrapFromCodex(baseDir string) (*pivot.PivotFile, error) {
	agentFiles, _ := filepath.Glob(filepath.Join(baseDir, "agents", "*.toml"))
	promptFiles, _ := filepath.Glob(filepath.Join(baseDir, "prompts", "*.md"))
	if len(agentFiles) == 0 && len(promptFiles) == 0 {
		return nil, nil
	}
	sort.Strings(agentFiles)
	sort.Strings(promptFiles)

	pf := &pivot.PivotFile{Version: "1"}
	normalizedAgents := map[string]string{}
	for _, path := range agentFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var native codexAgentFile
		if err := toml.Unmarshal(data, &native); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		if native.Name == "" {
			native.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		id, err := normalizeCodexID(native.Name)
		if err != nil {
			return nil, err
		}
		if previous, exists := normalizedAgents[id]; exists {
			return nil, fmt.Errorf("Codex agent names %q and %q both normalize to %q", previous, native.Name, id)
		}
		normalizedAgents[id] = native.Name
		agent := pivot.AgentDefinition{ID: id, Description: native.Description, Mode: "subagent"}
		instructions, skills := splitCodexSkillHint(native.DeveloperInstructions)
		agent.SystemPrompt, agent.Skills = instructions, skills
		codex := map[string]any{}
		if native.Name != id {
			codex["name"] = native.Name
		}
		if native.Model != "" {
			codex["model"] = native.Model
		}
		if native.ModelReasoningEffort != "" {
			codex["modelReasoningEffort"] = native.ModelReasoningEffort
		}
		if native.SandboxMode != "" {
			codex["sandboxMode"] = native.SandboxMode
		}
		if native.ApprovalPolicy != "" {
			codex["approvalPolicy"] = native.ApprovalPolicy
		}
		if native.WebSearch != "" {
			codex["webSearch"] = native.WebSearch
		}
		if len(native.NicknameCandidates) > 0 {
			values := make([]any, len(native.NicknameCandidates))
			for i, nickname := range native.NicknameCandidates {
				values[i] = nickname
			}
			codex["nicknameCandidates"] = values
		}
		if len(codex) > 0 {
			agent.Extensions = map[string]any{"codex": codex}
		}
		agent.Permissions = codexPermissions(native)
		pf.Agents = append(pf.Agents, agent)
	}

	normalizedCommands := map[string]string{}
	for _, path := range promptFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		frontmatter, body, err := splitFrontmatter(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		nativeName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		id, err := normalizeCodexID(nativeName)
		if err != nil {
			return nil, err
		}
		if previous, exists := normalizedCommands[id]; exists {
			return nil, fmt.Errorf("Codex prompt names %q and %q both normalize to %q", previous, nativeName, id)
		}
		normalizedCommands[id] = nativeName
		cmd := pivot.CommandDefinition{ID: id, Description: stringValue(frontmatter["description"]), Template: body}
		if nativeName != id {
			cmd.Extensions = map[string]any{"codex": map[string]any{"name": nativeName}}
		}
		const delegationPrefix = "Delegate this task to the `"
		if strings.HasPrefix(body, delegationPrefix) {
			rest := strings.TrimPrefix(body, delegationPrefix)
			if end := strings.Index(rest, "` custom agent."); end >= 0 {
				nativeAgent := rest[:end]
				matched := false
				for pivotID, name := range normalizedAgents {
					if name == nativeAgent {
						cmd.Agent = pivotID
						matched = true
						break
					}
				}
				if matched {
					body = strings.TrimLeft(rest[end+len("` custom agent."):], "\n")
					cmd.Template = body
				}
			}
		}
		pf.Commands = append(pf.Commands, cmd)
	}
	return pf, nil
}

func normalizeCodexID(name string) (string, error) {
	id := strings.ReplaceAll(name, "_", "-")
	if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(id) {
		return "", fmt.Errorf("Codex name %q cannot be represented as a Shenron id", name)
	}
	return id, nil
}

func splitCodexSkillHint(instructions string) (string, []string) {
	const marker = "\n\nWhen applicable, use these skills: "
	index := strings.LastIndex(instructions, marker)
	if index < 0 || !strings.HasSuffix(instructions, ".") {
		return instructions, nil
	}
	list := strings.TrimSuffix(instructions[index+len(marker):], ".")
	var skills []string
	for _, item := range strings.Split(list, ", ") {
		skill := strings.TrimPrefix(item, "$")
		if regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(skill) {
			skills = append(skills, skill)
		}
	}
	if len(skills) == 0 {
		return instructions, nil
	}
	return strings.TrimRight(instructions[:index], "\n"), skills
}

func codexPermissions(native codexAgentFile) *pivot.Permissions {
	perms := &pivot.Permissions{}
	switch native.SandboxMode {
	case "workspace-write":
		perms.Edit = "allow"
	case "read-only":
		if native.ApprovalPolicy == "on-request" {
			perms.Edit = "ask"
		} else if native.ApprovalPolicy == "never" {
			perms.Edit = "deny"
		}
	}
	if native.WebSearch != "" {
		if native.WebSearch == "disabled" {
			perms.WebSearch = "deny"
		} else {
			perms.WebSearch = "allow"
		}
	}
	if perms.Edit == "" && perms.WebSearch == "" {
		return nil
	}
	return perms
}

func bootstrapFromOpenCode(baseDir string) (*pivot.PivotFile, error) {
	configPath := filepath.Join(baseDir, "opencode.json")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read opencode config: %w", err)
	}

	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse opencode.json: %w", err)
	}

	pf := &pivot.PivotFile{Version: "1"}

	if agents, ok := root["agent"].(map[string]any); ok {
		for _, id := range sortedKeys(agents) {
			fragment, ok := agents[id].(map[string]any)
			if !ok {
				continue
			}
			agent, err := openCodeAgentToPivot(id, fragment, baseDir)
			if err != nil {
				return nil, err
			}
			pf.Agents = append(pf.Agents, agent)
		}
	}

	if commands, ok := root["command"].(map[string]any); ok {
		for _, id := range sortedKeys(commands) {
			fragment, ok := commands[id].(map[string]any)
			if !ok {
				continue
			}
			cmd, err := openCodeCommandToPivot(id, fragment, baseDir)
			if err != nil {
				return nil, err
			}
			pf.Commands = append(pf.Commands, cmd)
		}
	}

	if len(pf.Agents) == 0 && len(pf.Commands) == 0 {
		return nil, nil
	}
	return pf, nil
}

// ensureOpenCodeExtension returns the agent's extensions.opencode submap, creating
// it (and the parent extensions map) if absent, so callers can merge keys into it
// without clobbering values set by earlier callers.
func ensureOpenCodeExtension(agent *pivot.AgentDefinition) map[string]any {
	if agent.Extensions == nil {
		agent.Extensions = map[string]any{}
	}
	opencode, ok := agent.Extensions["opencode"].(map[string]any)
	if !ok {
		opencode = map[string]any{}
		agent.Extensions["opencode"] = opencode
	}
	return opencode
}

func openCodeAgentToPivot(id string, fragment map[string]any, baseDir string) (pivot.AgentDefinition, error) {
	agent := pivot.AgentDefinition{
		ID:          id,
		Description: stringValue(fragment["description"]),
		Mode:        "primary",
	}

	if mode := stringValue(fragment["mode"]); mode != "" {
		agent.Mode = mode
	}
	if temp, ok := fragment["temperature"].(float64); ok {
		agent.Temperature = &temp
	}
	if skills := stringSlice(fragment["skills"]); len(skills) > 0 {
		agent.Skills = skills
	}

	if prompt := stringValue(fragment["prompt"]); prompt != "" {
		content, err := readOpenCodeFileRef(prompt, baseDir)
		if err != nil {
			return agent, err
		}
		if strings.TrimSpace(content) != "" {
			agent.SystemPrompt = content
		}
	}

	if perms, ok := fragment["permission"].(map[string]any); ok {
		agent.Permissions = openCodePermissionsToPivot(perms)
	}

	if model := stringValue(fragment["model"]); model != "" {
		ensureOpenCodeExtension(&agent)["model"] = model
	}

	if steps, ok := fragment["steps"]; ok {
		ensureOpenCodeExtension(&agent)["steps"] = steps
	}

	if perms, ok := fragment["permission"].(map[string]any); ok {
		if override := openCodePermissionOverride(perms); len(override) > 0 {
			ensureOpenCodeExtension(&agent)["permission"] = override
		}
	}

	return agent, nil
}

func openCodeCommandToPivot(id string, fragment map[string]any, baseDir string) (pivot.CommandDefinition, error) {
	cmd := pivot.CommandDefinition{
		ID:          id,
		Description: stringValue(fragment["description"]),
		Agent:       stringValue(fragment["agent"]),
		Model:       stringValue(fragment["model"]),
	}

	if template := stringValue(fragment["template"]); template != "" {
		content, err := readOpenCodeFileRef(template, baseDir)
		if err != nil {
			return cmd, err
		}
		cmd.Template = content
	}

	return cmd, nil
}

func readOpenCodeFileRef(ref, baseDir string) (string, error) {
	m := fileRefPattern.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return ref, nil
	}
	path := strings.TrimPrefix(m[1], "./")
	data, err := os.ReadFile(filepath.Join(baseDir, path))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read referenced file %q: %w", path, err)
	}
	return string(data), nil
}

func openCodePermissionsToPivot(perms map[string]any) *pivot.Permissions {
	out := &pivot.Permissions{}
	readSubs := []string{"glob", "grep", "list", "lsp"}
	readVals := map[string]string{}

	for _, key := range readSubs {
		if val := stringValue(perms[key]); val != "" {
			readVals[key] = val
		}
	}
	if len(readVals) > 0 {
		if allSame(readVals) {
			out.Read = firstValue(readVals)
		}
	}

	if val := stringValue(perms["edit"]); val != "" {
		out.Edit = val
	}
	if val := stringValue(perms["webfetch"]); val != "" {
		out.WebFetch = val
	}
	if val := stringValue(perms["websearch"]); val != "" {
		out.WebSearch = val
	}
	if bash, ok := perms["bash"]; ok {
		out.Bash = bash
	}
	if tasks, ok := perms["task"].(map[string]any); ok {
		out.Tasks = stringMap(tasks)
	}

	if out.Read == "" && out.Edit == "" && out.Bash == nil && out.WebFetch == "" && out.WebSearch == "" && len(out.Tasks) == 0 {
		return nil
	}
	return out
}

func openCodePermissionOverride(perms map[string]any) map[string]string {
	readSubs := []string{"glob", "grep", "list", "lsp"}
	out := map[string]string{}
	for _, key := range readSubs {
		if val := stringValue(perms[key]); val != "" {
			out[key] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func bootstrapFromClaude(baseDir string) (*pivot.PivotFile, error) {
	agentsDir := filepath.Join(baseDir, "agents")
	commandsDir := filepath.Join(baseDir, "commands")

	agentFiles, _ := filepath.Glob(filepath.Join(agentsDir, "*.md"))
	commandFiles, _ := filepath.Glob(filepath.Join(commandsDir, "*.md"))
	if len(agentFiles) == 0 && len(commandFiles) == 0 {
		return nil, nil
	}

	pf := &pivot.PivotFile{Version: "1"}
	sort.Strings(agentFiles)
	sort.Strings(commandFiles)

	for _, path := range agentFiles {
		agent, err := claudeAgentFileToPivot(path)
		if err != nil {
			return nil, err
		}
		pf.Agents = append(pf.Agents, agent)
	}

	for _, path := range commandFiles {
		cmd, err := claudeCommandFileToPivot(path)
		if err != nil {
			return nil, err
		}
		pf.Commands = append(pf.Commands, cmd)
	}

	return pf, nil
}

func claudeAgentFileToPivot(path string) (pivot.AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pivot.AgentDefinition{}, err
	}

	fm, body, err := splitFrontmatter(string(data))
	if err != nil {
		return pivot.AgentDefinition{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	id := stringValue(fm["name"])
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	agent := pivot.AgentDefinition{
		ID:           id,
		Description:  stringValue(fm["description"]),
		Mode:         "primary",
		SystemPrompt: strings.TrimSpace(body),
		Permissions:  claudeToolsToPermissions(fm),
		Skills:       stringSlice(fm["skills"]),
	}

	if model := stringValue(fm["model"]); model != "" {
		agent.Extensions = map[string]any{
			"claude": map[string]any{"model": model},
		}
	}

	return agent, nil
}

func claudeCommandFileToPivot(path string) (pivot.CommandDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pivot.CommandDefinition{}, err
	}

	content := string(data)
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	cmd := pivot.CommandDefinition{ID: id}

	lines := strings.Split(content, "\n")
	start := 0
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "<!-- agent:") {
		agentRef := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "<!-- agent:")), "-->")
		cmd.Agent = strings.TrimSpace(agentRef)
		start = 1
	}
	cmd.Template = strings.TrimLeft(strings.Join(lines[start:], "\n"), "\n")
	return cmd, nil
}

func claudeToolsToPermissions(fm map[string]any) *pivot.Permissions {
	tools := stringSlice(fm["tools"])
	if len(tools) == 0 && stringValue(fm["permissionMode"]) == "" {
		return nil
	}

	perms := &pivot.Permissions{}
	for _, tool := range tools {
		switch tool {
		case "Read":
			perms.Read = "allow"
		case "Bash":
			perms.Bash = "allow"
		case "WebFetch":
			perms.WebFetch = "allow"
		case "WebSearch":
			perms.WebSearch = "allow"
		}
	}

	switch stringValue(fm["permissionMode"]) {
	case "acceptEdits":
		perms.Edit = "allow"
	case "plan":
		perms.Edit = "deny"
	case "default":
		if perms.Edit == "" {
			perms.Edit = "ask"
		}
	}

	if perms.Read == "" && perms.Edit == "" && perms.Bash == nil && perms.WebFetch == "" && perms.WebSearch == "" {
		return nil
	}
	return perms
}

func splitFrontmatter(content string) (map[string]any, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(content, "---") {
		return map[string]any{}, content, nil
	}

	rest := content[len("---"):]
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("unclosed frontmatter")
	}

	fmRaw := rest[:end]
	body := strings.TrimLeft(rest[end+len("\n---"):], "\r\n")

	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter: %w", err)
	}
	return fm, body, nil
}

func marshalPivot(pf *pivot.PivotFile) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(pf); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(v)
	}
}

func stringSlice(v any) []string {
	switch s := v.(type) {
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str := stringValue(item); str != "" {
				out = append(out, str)
			}
		}
		return out
	case []string:
		return s
	default:
		return nil
	}
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = stringValue(v)
	}
	return out
}

func allSame(m map[string]string) bool {
	var first string
	for _, v := range m {
		if first == "" {
			first = v
		} else if v != first {
			return false
		}
	}
	return true
}

func firstValue(m map[string]string) string {
	for _, v := range m {
		return v
	}
	return ""
}
