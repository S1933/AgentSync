package pivot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var idRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var validPermissionValues = map[string]bool{
	"allow": true,
	"deny":  true,
	"ask":   true,
}

var validOpenCodePermissionKeys = map[string]bool{
	"glob": true,
	"grep": true,
	"list": true,
	"lsp":  true,
}

var validCodexReasoningEfforts = map[string]bool{"minimal": true, "low": true, "medium": true, "high": true, "xhigh": true}
var validCodexSandboxModes = map[string]bool{"read-only": true, "workspace-write": true, "danger-full-access": true}
var validCodexApprovalPolicies = map[string]bool{"untrusted": true, "on-request": true, "never": true}
var validCodexWebSearchModes = map[string]bool{"disabled": true, "cached": true, "indexed": true, "live": true}
var codexNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Parse parses YAML pivot data and validates it against the schema.
func Parse(data []byte, pivotDir string) (*PivotFile, error) {
	var pf PivotFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	return validateParsedPivot(&pf, pivotDir)
}

// ParseStrict parses YAML pivot data with unknown schema fields rejected. It is
// intended for untrusted, installed configuration packages; Parse remains
// permissive for backwards compatibility with existing user pivots.
func ParseStrict(data []byte, pivotDir string) (*PivotFile, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)

	var pf PivotFile
	if err := decoder.Decode(&pf); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	return validateParsedPivot(&pf, pivotDir)
}

func validateParsedPivot(pf *PivotFile, pivotDir string) (*PivotFile, error) {
	var errs []string
	errs = append(errs, validatePivot(pf, pivotDir)...)

	if len(errs) > 0 {
		return nil, fmt.Errorf("validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return pf, nil
}

func validatePivot(pf *PivotFile, pivotDir string) []string {
	var errs []string

	if pf.Version == "" {
		errs = append(errs, "version is required")
	}

	agentIDs := make(map[string]int)
	for i, agent := range pf.Agents {
		prefix := fmt.Sprintf("agents[%d]", i)
		if agent.ID != "" {
			if first, seen := agentIDs[agent.ID]; seen {
				errs = append(errs, fmt.Sprintf("%s.id duplicates agents[%d].id (%q)", prefix, first, agent.ID))
			} else {
				agentIDs[agent.ID] = i
			}
		}
		errs = append(errs, validateAgent(agent, prefix, pivotDir)...)
	}

	commandIDs := make(map[string]int)
	for i, cmd := range pf.Commands {
		prefix := fmt.Sprintf("commands[%d]", i)
		if cmd.ID != "" {
			if first, seen := commandIDs[cmd.ID]; seen {
				errs = append(errs, fmt.Sprintf("%s.id duplicates commands[%d].id (%q)", prefix, first, cmd.ID))
			} else {
				commandIDs[cmd.ID] = i
			}
		}
		errs = append(errs, validateCommand(cmd, prefix, agentIDsToSet(agentIDs))...)
	}
	errs = append(errs, validateUniqueCodexNames(pf)...)

	return errs
}

func validateUniqueCodexNames(pf *PivotFile) []string {
	var errs []string
	agentNames := map[string]int{}
	for i, agent := range pf.Agents {
		name := codexResolvedName(agent.ID, agent.Extensions)
		if first, exists := agentNames[name]; exists {
			errs = append(errs, fmt.Sprintf("agents[%d].extensions.codex.name: duplicate Codex native agent name from agents[%d] (%q)", i, first, name))
			continue
		}
		agentNames[name] = i
	}
	commandNames := map[string]int{}
	for i, command := range pf.Commands {
		name := codexResolvedName(command.ID, command.Extensions)
		if first, exists := commandNames[name]; exists {
			errs = append(errs, fmt.Sprintf("commands[%d].extensions.codex.name: duplicate Codex native command name from commands[%d] (%q)", i, first, name))
			continue
		}
		commandNames[name] = i
	}
	return errs
}

func codexResolvedName(id string, extensions map[string]any) string {
	if codex, ok := extensions["codex"].(map[string]any); ok {
		if name, ok := codex["name"].(string); ok && name != "" {
			return name
		}
	}
	return id
}

func agentIDsToSet(ids map[string]int) map[string]bool {
	set := make(map[string]bool, len(ids))
	for id := range ids {
		set[id] = true
	}
	return set
}

func validateAgent(agent AgentDefinition, prefix, pivotDir string) []string {
	var errs []string

	if agent.ID == "" {
		errs = append(errs, prefix+".id is required")
	} else if !idRegex.MatchString(agent.ID) {
		errs = append(errs, prefix+".id must match ^[a-z][a-z0-9-]*$")
	}

	if agent.Description == "" {
		errs = append(errs, prefix+".description is required")
	} else if len(agent.Description) > 1024 {
		errs = append(errs, prefix+".description must be 1-1024 characters")
	}

	if agent.Mode == "" {
		errs = append(errs, prefix+".mode is required")
	} else if agent.Mode != "primary" && agent.Mode != "subagent" {
		errs = append(errs, prefix+".mode must be primary or subagent")
	}

	hasSystemPrompt := strings.TrimSpace(agent.SystemPrompt) != ""
	hasPromptFile := strings.TrimSpace(agent.PromptFile) != ""
	if hasSystemPrompt && hasPromptFile {
		errs = append(errs, prefix+": systemPrompt and promptFile are mutually exclusive")
	}

	if hasPromptFile {
		resolved := filepath.Join(pivotDir, agent.PromptFile)
		if _, err := os.Stat(resolved); err != nil {
			errs = append(errs, fmt.Sprintf("%s.promptFile: file not found: %s", prefix, agent.PromptFile))
		}
	}

	if agent.Temperature != nil {
		t := *agent.Temperature
		if t < 0.0 || t > 2.0 {
			errs = append(errs, prefix+".temperature must be between 0.0 and 2.0")
		}
	}

	if agent.Permissions != nil {
		errs = append(errs, validatePermissions(agent.Permissions, prefix+".permissions")...)
	}

	if agent.Extensions != nil {
		errs = append(errs, validateExtensions(agent.Extensions, prefix+".extensions")...)
	}

	for i, name := range agent.Skills {
		if !idRegex.MatchString(name) {
			errs = append(errs, fmt.Sprintf("%s.skills[%d] must match ^[a-z][a-z0-9-]*$", prefix, i))
		}
	}

	return errs
}

func validatePermissions(perms *Permissions, prefix string) []string {
	var errs []string

	errs = append(errs, validatePermissionEnum(perms.Read, prefix+".read")...)
	errs = append(errs, validatePermissionEnum(perms.Edit, prefix+".edit")...)
	errs = append(errs, validatePermissionEnum(perms.WebFetch, prefix+".webfetch")...)
	errs = append(errs, validatePermissionEnum(perms.WebSearch, prefix+".websearch")...)
	errs = append(errs, validateBashPermission(perms.Bash, prefix+".bash")...)

	for key, val := range perms.Tasks {
		errs = append(errs, validatePermissionEnum(val, fmt.Sprintf("%s.tasks.%s", prefix, key))...)
	}

	return errs
}

func validateBashPermission(bash any, prefix string) []string {
	if bash == nil {
		return nil
	}

	switch v := bash.(type) {
	case string:
		return validatePermissionEnum(v, prefix)
	case map[string]any:
		var errs []string
		for key, val := range v {
			s, ok := val.(string)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.%s must be a string", prefix, key))
				continue
			}
			errs = append(errs, validatePermissionEnum(s, fmt.Sprintf("%s.%s", prefix, key))...)
		}
		return errs
	default:
		return []string{prefix + " must be a string or map[string]string"}
	}
}

func validatePermissionEnum(value, field string) []string {
	if value == "" {
		return nil
	}
	if !validPermissionValues[value] {
		return []string{fmt.Sprintf("%s must be allow, deny, or ask (got %q)", field, value)}
	}
	return nil
}

func validateExtensions(extensions map[string]any, prefix string) []string {
	var errs []string
	errs = append(errs, validateCodexExtensions(extensions, prefix, false)...)
	opencode, ok := extensions["opencode"]
	if !ok {
		return errs
	}

	opencodeMap, ok := opencode.(map[string]any)
	if !ok {
		return append(errs, prefix+".opencode must be an object")
	}

	permission, ok := opencodeMap["permission"]
	if !ok {
		return errs
	}

	permMap, ok := permission.(map[string]any)
	if !ok {
		return append(errs, prefix+".opencode.permission must be an object")
	}

	for key, val := range permMap {
		if !validOpenCodePermissionKeys[key] {
			errs = append(errs, fmt.Sprintf("%s.opencode.permission.%s: invalid key (must be glob, grep, list, or lsp)", prefix, key))
			continue
		}
		s, ok := val.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.opencode.permission.%s must be a string", prefix, key))
			continue
		}
		errs = append(errs, validatePermissionEnum(s, fmt.Sprintf("%s.opencode.permission.%s", prefix, key))...)
	}

	return errs
}

func validateCodexExtensions(extensions map[string]any, prefix string, command bool) []string {
	codex, ok := extensions["codex"]
	if !ok {
		return nil
	}
	values, ok := codex.(map[string]any)
	if !ok {
		return []string{prefix + ".codex must be an object"}
	}

	var errs []string
	if name, ok := values["name"]; ok {
		s, ok := name.(string)
		if !ok || !codexNameRegex.MatchString(s) {
			errs = append(errs, prefix+".codex.name must match ^[a-z][a-z0-9_-]*$")
		}
	}
	if command {
		return errs
	}
	for key, allowed := range map[string]map[string]bool{
		"modelReasoningEffort": validCodexReasoningEfforts,
		"sandboxMode":          validCodexSandboxModes,
		"approvalPolicy":       validCodexApprovalPolicies,
		"webSearch":            validCodexWebSearchModes,
	} {
		if value, ok := values[key]; ok {
			s, isString := value.(string)
			if !isString || !allowed[s] {
				errs = append(errs, fmt.Sprintf("%s.codex.%s has an invalid value", prefix, key))
			}
		}
	}
	if model, ok := values["model"]; ok {
		if s, ok := model.(string); !ok || strings.TrimSpace(s) == "" {
			errs = append(errs, prefix+".codex.model must be a non-empty string")
		}
	}
	if raw, ok := values["nicknameCandidates"]; ok {
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			errs = append(errs, prefix+".codex.nicknameCandidates must be a non-empty string list")
		} else {
			seen := map[string]bool{}
			for i, item := range list {
				s, ok := item.(string)
				if !ok || strings.TrimSpace(s) == "" || seen[s] {
					errs = append(errs, fmt.Sprintf("%s.codex.nicknameCandidates[%d] must be a unique non-empty string", prefix, i))
					continue
				}
				seen[s] = true
			}
		}
	}
	return errs
}

func validateCommand(cmd CommandDefinition, prefix string, agentIDs map[string]bool) []string {
	var errs []string

	if cmd.ID == "" {
		errs = append(errs, prefix+".id is required")
	} else if !idRegex.MatchString(cmd.ID) {
		errs = append(errs, prefix+".id must match ^[a-z][a-z0-9-]*$")
	}

	if cmd.Description == "" {
		errs = append(errs, prefix+".description is required")
	}

	if strings.TrimSpace(cmd.Template) == "" {
		errs = append(errs, prefix+".template is required")
	}

	if cmd.Agent != "" && !agentIDs[cmd.Agent] {
		errs = append(errs, fmt.Sprintf("%s.agent references unknown agent %q", prefix, cmd.Agent))
	}
	if cmd.Extensions != nil {
		errs = append(errs, validateCodexExtensions(cmd.Extensions, prefix+".extensions", true)...)
	}

	return errs
}
