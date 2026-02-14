package workspacetpl

import (
	"embed"
	"fmt"
	"path"
)

// Template is a workspace bootstrap file and its relative destination path.
type Template struct {
	RelativePath string
	Content      string
}

var rootTemplateNames = []string{
	"AGENTS.md",
	"HOOKS.md",
	"SOUL.md",
	"TOOLS.md",
	"USER.md",
	"IDENTITY.md",
}

const memoryTemplateName = "MEMORY.md"

//go:embed templates/workspace/*.md
var workspaceTemplates embed.FS

// Load returns workspace templates used during onboard initialization.
func Load() ([]Template, error) {
	templates := make([]Template, 0, len(rootTemplateNames)+1)

	for _, name := range rootTemplateNames {
		content, err := readTemplate(name)
		if err != nil {
			return nil, err
		}
		templates = append(templates, Template{RelativePath: name, Content: content})
	}

	memoryContent, err := readTemplate(memoryTemplateName)
	if err != nil {
		return nil, err
	}
	templates = append(templates, Template{RelativePath: path.Join("memory", memoryTemplateName), Content: memoryContent})

	return templates, nil
}

func readTemplate(name string) (string, error) {
	content, err := workspaceTemplates.ReadFile(path.Join("templates", "workspace", name))
	if err != nil {
		return "", fmt.Errorf("read workspace template %s: %w", name, err)
	}
	return string(content), nil
}
