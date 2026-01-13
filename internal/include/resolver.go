package include

import (
	"os"
	"path/filepath"
	"strings"
)

func ResolvePath(basePath, includePath string) string {
	if strings.HasPrefix(includePath, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if includePath == "~" {
				return home
			}
			includePath = filepath.Join(home, includePath[2:])
		}
	}

	if filepath.IsAbs(includePath) {
		return filepath.Clean(includePath)
	}

	baseDir := filepath.Dir(basePath)
	resolved := filepath.Join(baseDir, includePath)
	return filepath.Clean(resolved)
}
