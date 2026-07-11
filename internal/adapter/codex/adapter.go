package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
	"github.com/pelletier/go-toml/v2"
)

// Adapter renders Shenron definitions into Codex custom-agent and custom-prompt files.
type Adapter struct {
	baseDir     string
	pivotDir    string
	nativeNames map[string]string
}

type agentFile struct {
	Name                  string   `toml:"name"`
	Description           string   `toml:"description"`
	Model                 string   `toml:"model,omitempty"`
	ModelReasoningEffort  string   `toml:"model_reasoning_effort,omitempty"`
	SandboxMode           string   `toml:"sandbox_mode,omitempty"`
	ApprovalPolicy        string   `toml:"approval_policy,omitempty"`
	WebSearch             string   `toml:"web_search,omitempty"`
	NicknameCandidates    []string `toml:"nickname_candidates,omitempty"`
	DeveloperInstructions string   `toml:"developer_instructions"`
}

func NewAdapter() *Adapter { return NewAdapterWithBaseDir(fsutil.CodexPath(), "") }

func NewAdapterWithBaseDir(baseDir, pivotDir string) *Adapter {
	return &Adapter{baseDir: baseDir, pivotDir: pivotDir, nativeNames: map[string]string{}}
}

func (a *Adapter) Name() string { return "codex" }

func (a *Adapter) SetPivotDir(dir string) {
	a.pivotDir = dir
	a.nativeNames = map[string]string{}
}

func (a *Adapter) ValidateAgent(agent pivot.AgentDefinition) error {
	if agent.Mode != "primary" && agent.Mode != "subagent" {
		return fmt.Errorf("agent %q: mode must be primary or subagent", agent.ID)
	}
	return nil
}

func (a *Adapter) GenerateAgent(agent pivot.AgentDefinition) (map[string]string, error) {
	if err := a.ValidateAgent(agent); err != nil {
		return nil, err
	}
	nativeName := codexName(agent.ID, agent.Extensions)
	a.nativeNames[agent.ID] = nativeName
	instructions, err := resolveInstructions(agent, a.pivotDir)
	if err != nil {
		return nil, err
	}

	native := agentFile{
		Name: nativeName, Description: agent.Description, Model: resolveModel(agent),
		ModelReasoningEffort: codexString(agent.Extensions, "modelReasoningEffort"),
		SandboxMode:          resolveSandbox(agent), ApprovalPolicy: resolveApproval(agent),
		WebSearch: resolveWebSearch(agent), NicknameCandidates: codexStrings(agent.Extensions, "nicknameCandidates"),
		DeveloperInstructions: instructions,
	}
	data, err := toml.Marshal(native)
	if err != nil {
		return nil, fmt.Errorf("marshal Codex agent: %w", err)
	}
	return map[string]string{filepath.Join(a.baseDir, "agents", nativeName+".toml"): string(data)}, nil
}

func (a *Adapter) GenerateCommand(cmd pivot.CommandDefinition) (map[string]string, error) {
	nativeAgent := cmd.Agent
	if name, ok := a.nativeNames[cmd.Agent]; ok {
		nativeAgent = name
	}
	var content strings.Builder
	content.WriteString("---\ndescription: ")
	content.WriteString(strconv.Quote(cmd.Description))
	content.WriteString("\n---\n\n")
	if nativeAgent != "" {
		content.WriteString("Delegate this task to the `")
		content.WriteString(nativeAgent)
		content.WriteString("` custom agent.\n\n")
	}
	content.WriteString(cmd.Template)
	return map[string]string{filepath.Join(a.baseDir, "prompts", codexName(cmd.ID, cmd.Extensions)+".md"): content.String()}, nil
}

func (a *Adapter) TargetPaths() []string {
	return []string{filepath.Join(a.baseDir, "agents"), filepath.Join(a.baseDir, "prompts")}
}
func (a *Adapter) MergeFile(string, []byte, map[string]any) ([]byte, error) { return nil, nil }

func resolveInstructions(agent pivot.AgentDefinition, pivotDir string) (string, error) {
	instructions := agent.SystemPrompt
	if strings.TrimSpace(instructions) == "" && agent.PromptFile != "" {
		data, err := os.ReadFile(filepath.Join(pivotDir, agent.PromptFile))
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		instructions = string(data)
	}
	if strings.TrimSpace(instructions) == "" {
		instructions = agent.Description
	}
	if len(agent.Skills) > 0 {
		prefixed := make([]string, 0, len(agent.Skills))
		for _, skill := range agent.Skills {
			prefixed = append(prefixed, "$"+skill)
		}
		instructions = strings.TrimRight(instructions, "\n") + "\n\nWhen applicable, use these skills: " + strings.Join(prefixed, ", ") + "."
	}
	return instructions, nil
}

func codexExtension(extensions map[string]any) map[string]any {
	if extensions == nil {
		return nil
	}
	value, _ := extensions["codex"].(map[string]any)
	return value
}
func codexString(extensions map[string]any, key string) string {
	value := codexExtension(extensions)
	if value == nil {
		return ""
	}
	s, _ := value[key].(string)
	return s
}
func codexStrings(extensions map[string]any, key string) []string {
	value := codexExtension(extensions)
	if value == nil {
		return nil
	}
	switch list := value[key].(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
func codexName(id string, extensions map[string]any) string {
	if name := codexString(extensions, "name"); name != "" {
		return name
	}
	return id
}
func resolveModel(agent pivot.AgentDefinition) string {
	if model := codexString(agent.Extensions, "model"); model != "" {
		return model
	}
	return agent.Model
}
func resolveSandbox(agent pivot.AgentDefinition) string {
	if sandbox := codexString(agent.Extensions, "sandboxMode"); sandbox != "" {
		return sandbox
	}
	if agent.Permissions == nil {
		return ""
	}
	switch agent.Permissions.Edit {
	case "allow":
		return "workspace-write"
	case "ask", "deny":
		return "read-only"
	default:
		return ""
	}
}
func resolveApproval(agent pivot.AgentDefinition) string {
	if approval := codexString(agent.Extensions, "approvalPolicy"); approval != "" {
		return approval
	}
	if agent.Permissions == nil {
		return ""
	}
	switch agent.Permissions.Edit {
	case "ask":
		return "on-request"
	case "deny":
		return "never"
	}
	if bash, ok := agent.Permissions.Bash.(string); ok && bash == "ask" {
		return "on-request"
	}
	return ""
}
func resolveWebSearch(agent pivot.AgentDefinition) string {
	if search := codexString(agent.Extensions, "webSearch"); search != "" {
		return search
	}
	if agent.Permissions == nil {
		return ""
	}
	switch agent.Permissions.WebSearch {
	case "allow":
		return "live"
	case "ask", "deny":
		return "disabled"
	default:
		return ""
	}
}
