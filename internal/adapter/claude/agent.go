package claude

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
	"gopkg.in/yaml.v3"
)

type agentFrontmatter struct {
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Model          string   `yaml:"model,omitempty"`
	Tools          []string `yaml:"tools,omitempty"`
	PermissionMode string   `yaml:"permissionMode"`
}

// GenerateAgent produces a Claude Code agent Markdown file with YAML frontmatter.
func GenerateAgent(agent pivot.AgentDefinition, pivotDir string) (map[string]string, error) {
	return generateAgentFile(agent, pivotDir, fsutil.ClaudePath())
}

func generateAgentFile(agent pivot.AgentDefinition, pivotDir, baseDir string) (map[string]string, error) {
	body, err := resolvePromptContent(agent, pivotDir)
	if err != nil {
		return nil, err
	}

	fm := agentFrontmatter{
		Name:           agent.ID,
		Description:    agent.Description,
		Model:          agent.Model,
		Tools:          mapTools(agent.Permissions),
		PermissionMode: mapPermissionMode(agent.Permissions),
	}

	fmYAML, err := marshalFrontmatter(&fm)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmYAML)
	buf.WriteByte('\n')
	buf.WriteString("---\n\n")
	buf.WriteString(body)

	agentPath := filepath.Join(baseDir, "agents", agent.ID+".md")
	return map[string]string{agentPath: buf.String()}, nil
}

func marshalFrontmatter(fm *agentFrontmatter) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("marshal frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close frontmatter encoder: %w", err)
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func resolvePromptContent(agent pivot.AgentDefinition, pivotDir string) (string, error) {
	if strings.TrimSpace(agent.SystemPrompt) != "" {
		return agent.SystemPrompt, nil
	}
	if agent.PromptFile != "" {
		data, err := os.ReadFile(filepath.Join(pivotDir, agent.PromptFile))
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func mapTools(perms *pivot.Permissions) []string {
	if perms == nil {
		return nil
	}

	var tools []string
	if perms.Read == "allow" {
		tools = append(tools, "Read")
	}
	if hasBashAllow(perms.Bash) {
		tools = append(tools, "Bash")
	}
	if perms.WebFetch == "allow" {
		tools = append(tools, "WebFetch")
	}
	if perms.WebSearch == "allow" {
		tools = append(tools, "WebSearch")
	}
	if hasTaskAllow(perms.Tasks) {
		tools = append(tools, "Task")
	}
	return tools
}

func mapPermissionMode(perms *pivot.Permissions) string {
	if perms == nil || perms.Edit == "" {
		return "default"
	}
	switch perms.Edit {
	case "allow":
		return "acceptEdits"
	case "ask":
		return "default"
	case "deny":
		return "plan"
	default:
		return "default"
	}
}

func hasBashAllow(bash any) bool {
	switch v := bash.(type) {
	case string:
		return v == "allow"
	case map[string]string:
		for _, val := range v {
			if val == "allow" {
				return true
			}
		}
	case map[string]any:
		for _, val := range v {
			if s, ok := val.(string); ok && s == "allow" {
				return true
			}
		}
	}
	return false
}

func hasTaskAllow(tasks map[string]string) bool {
	for _, val := range tasks {
		if val == "allow" {
			return true
		}
	}
	return false
}
