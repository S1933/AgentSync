package claude

import (
	"path/filepath"
	"strings"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
)

// GenerateCommand produces a Claude Code command Markdown file.
func GenerateCommand(cmd pivot.CommandDefinition) (map[string]string, error) {
	return generateCommandFile(cmd)
}

func generateCommandFile(cmd pivot.CommandDefinition) (map[string]string, error) {
	var buf strings.Builder
	if cmd.Agent != "" {
		buf.WriteString("<!-- agent: ")
		buf.WriteString(cmd.Agent)
		buf.WriteString(" -->\n")
	}
	buf.WriteString(cmd.Template)

	cmdPath := filepath.Join(fsutil.ClaudePath(), "commands", cmd.ID+".md")
	return map[string]string{cmdPath: buf.String()}, nil
}
