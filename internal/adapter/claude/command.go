package claude

import (
	"path/filepath"
	"strings"

	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
)

// GenerateCommand produces a Claude Code command Markdown file.
func GenerateCommand(cmd pivot.CommandDefinition) (map[string]string, error) {
	return generateCommandFile(cmd, fsutil.ClaudePath())
}

func generateCommandFile(cmd pivot.CommandDefinition, baseDir string) (map[string]string, error) {
	var buf strings.Builder
	if cmd.Agent != "" {
		buf.WriteString("<!-- agent: ")
		buf.WriteString(cmd.Agent)
		buf.WriteString(" -->\n")
	}
	buf.WriteString(cmd.Template)

	cmdPath := filepath.Join(baseDir, "commands", cmd.ID+".md")
	return map[string]string{cmdPath: buf.String()}, nil
}
