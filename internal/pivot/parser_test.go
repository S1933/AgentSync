package pivot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata")
}

func TestParseValidPivotFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testdataDir(t), "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	pf, err := Parse(data, testdataDir(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pf.Version != "1" {
		t.Errorf("version = %q, want %q", pf.Version, "1")
	}
	if len(pf.Agents) != 2 {
		t.Fatalf("agents count = %d, want 2", len(pf.Agents))
	}
	if pf.Agents[0].ID != "build" {
		t.Errorf("agents[0].id = %q, want build", pf.Agents[0].ID)
	}
	if pf.Agents[0].Mode != "primary" {
		t.Errorf("agents[0].mode = %q, want primary", pf.Agents[0].Mode)
	}
	if pf.Agents[0].Temperature == nil || *pf.Agents[0].Temperature != 0.7 {
		t.Errorf("agents[0].temperature = %v, want 0.7", pf.Agents[0].Temperature)
	}
	if pf.Agents[1].PromptFile != "prompts/review.md" {
		t.Errorf("agents[1].promptFile = %q, want prompts/review.md", pf.Agents[1].PromptFile)
	}
	if len(pf.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(pf.Commands))
	}
	if pf.Commands[0].Agent != "build" {
		t.Errorf("commands[0].agent = %q, want build", pf.Commands[0].Agent)
	}
	if len(pf.Skills) != 1 || pf.Skills[0].Name != "test-driven-development" {
		t.Errorf("skills = %+v, want test-driven-development", pf.Skills)
	}
}

func TestParseAgentSkills(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    skills:
      - test-driven-development
      - verification-before-completion`

	pf, err := Parse([]byte(yaml), testdataDir(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"test-driven-development", "verification-before-completion"}
	if len(pf.Agents[0].Skills) != len(want) {
		t.Fatalf("skills = %#v, want %#v", pf.Agents[0].Skills, want)
	}
	for i := range want {
		if pf.Agents[0].Skills[i] != want[i] {
			t.Errorf("skills[%d] = %q, want %q", i, pf.Agents[0].Skills[i], want[i])
		}
	}
}

func TestParseAgentRejectsInvalidSkillName(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    skills: ["Invalid Skill"]`

	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].skills[0] must match ^[a-z][a-z0-9-]*$") {
		t.Fatalf("expected skill name validation error, got: %v", err)
	}
}

func TestParseMissingVersion(t *testing.T) {
	_, err := Parse([]byte(`agents: []`), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("expected version error, got: %v", err)
	}
}

func TestParseAgentMissingID(t *testing.T) {
	yaml := `version: "1"
agents:
  - description: "test"
    mode: primary`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].id is required") {
		t.Fatalf("expected id error, got: %v", err)
	}
}

func TestParseAgentInvalidID(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"uppercase", "Build"},
		{"spaces", "build agent"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml := `version: "1"
agents:
  - id: ` + tc.id + `
    description: "test"
    mode: primary`
			_, err := Parse([]byte(yaml), testdataDir(t))
			if err == nil || !strings.Contains(err.Error(), "must match ^[a-z][a-z0-9-]*$") {
				t.Fatalf("expected id regex error, got: %v", err)
			}
		})
	}
}

func TestParseAgentMissingDescription(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    mode: primary`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].description is required") {
		t.Fatalf("expected description error, got: %v", err)
	}
}

func TestParseAgentMissingMode(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].mode is required") {
		t.Fatalf("expected mode error, got: %v", err)
	}
}

func TestParseAgentInvalidMode(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: invalid`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].mode must be primary or subagent") {
		t.Fatalf("expected mode value error, got: %v", err)
	}
}

func TestParseAgentBothSystemPromptAndPromptFile(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    systemPrompt: "hello"
    promptFile: prompts/review.md`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "systemPrompt and promptFile are mutually exclusive") {
		t.Fatalf("expected XOR error, got: %v", err)
	}
}

func TestParseAgentPromptFileNotFound(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    promptFile: prompts/missing.md`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "promptFile: file not found") {
		t.Fatalf("expected promptFile error, got: %v", err)
	}
}

func TestParseAgentTemperatureOutOfRange(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    temperature: 3.0`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "temperature must be between 0.0 and 2.0") {
		t.Fatalf("expected temperature error, got: %v", err)
	}
}

func TestParseCommandMissingRequiredFields(t *testing.T) {
	yaml := `version: "1"
agents: []
commands:
  - id: ship`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil {
		t.Fatal("expected error for missing command fields")
	}
	errMsg := err.Error()
	for _, want := range []string{"commands[0].description is required", "commands[0].template is required"} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("expected error containing %q, got: %v", want, err)
		}
	}
}

func TestParseCommandAgentReferencesNonExistent(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
commands:
  - id: ship
    description: "ship it"
    template: "go"
    agent: missing`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "references unknown agent") {
		t.Fatalf("expected agent reference error, got: %v", err)
	}
}

func TestParseDuplicateAgentIDs(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "first"
    mode: primary
  - id: build
    description: "second"
    mode: primary`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[1].id duplicates agents[0].id") {
		t.Fatalf("expected duplicate agent id error, got: %v", err)
	}
}

func TestParseDuplicateCommandIDs(t *testing.T) {
	yaml := `version: "1"
agents: []
commands:
  - id: ship
    description: "first"
    template: "go"
  - id: ship
    description: "second"
    template: "go"`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "commands[1].id duplicates commands[0].id") {
		t.Fatalf("expected duplicate command id error, got: %v", err)
	}
}

func TestParsePermissionInvalidEnum(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "test"
    mode: primary
    permissions:
      read: maybe`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "must be allow, deny, or ask") {
		t.Fatalf("expected permission enum error, got: %v", err)
	}
}

func TestParseCodexExtensionValidation(t *testing.T) {
	valid := `version: "1"
agents:
  - id: reviewer
    description: "Review changes"
    mode: subagent
    extensions:
      codex:
        name: code_reviewer
        modelReasoningEffort: high
        sandboxMode: read-only
        approvalPolicy: on-request
        webSearch: disabled
        nicknameCandidates: [Atlas, Delta]
commands:
  - id: review
    description: "Review"
    template: "Review it"
    extensions:
      codex:
        name: review_changes`
	if _, err := Parse([]byte(valid), testdataDir(t)); err != nil {
		t.Fatalf("valid Codex extensions: %v", err)
	}

	invalid := strings.Replace(valid, "modelReasoningEffort: high", "modelReasoningEffort: fastest", 1)
	_, err := Parse([]byte(invalid), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "agents[0].extensions.codex.modelReasoningEffort") {
		t.Fatalf("expected Codex extension error, got: %v", err)
	}
}

func TestParseRejectsDuplicateCodexNativeNames(t *testing.T) {
	yaml := `version: "1"
agents:
  - id: build
    description: "Build"
    mode: primary
    extensions:
      codex: {name: shared_role}
  - id: review
    description: "Review"
    mode: subagent
    extensions:
      codex: {name: shared_role}`
	_, err := Parse([]byte(yaml), testdataDir(t))
	if err == nil || !strings.Contains(err.Error(), "duplicate Codex native agent name") {
		t.Fatalf("expected duplicate Codex native-name error, got: %v", err)
	}
}
