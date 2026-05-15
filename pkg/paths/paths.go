package paths

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	appHomeOnce sync.Once
	appHomePath string
)

func AppHome() string {
	appHomeOnce.Do(func() {
		if env := strings.TrimSpace(os.Getenv("SCICLAW_HOME")); env != "" {
			appHomePath = expandHome(env)
			return
		}

		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			appHomePath = filepath.Join(".", "sciclaw")
			return
		}

		newPath := filepath.Join(home, "sciclaw")
		oldPath := filepath.Join(home, ".picoclaw")
		if _, err := os.Stat(filepath.Join(oldPath, "config.json")); err == nil {
			if _, err2 := os.Stat(filepath.Join(newPath, "config.json")); os.IsNotExist(err2) {
				appHomePath = oldPath
				return
			}
		}
		appHomePath = newPath
	})
	return appHomePath
}

func ResetForTest() {
	appHomeOnce = sync.Once{}
	appHomePath = ""
}

func ConfigPath() string {
	return filepath.Join(AppHome(), "config.json")
}

func AuthPath() string {
	return filepath.Join(AppHome(), "auth.json")
}

func BackupsDir() string {
	return filepath.Join(AppHome(), "backups")
}

func TemplatesDir() string {
	return filepath.Join(AppHome(), "templates")
}

func GlobalSkillsDir() string {
	return filepath.Join(AppHome(), "global-skills")
}

func expandHome(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
