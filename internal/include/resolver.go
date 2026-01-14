package include

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathTraversal = errors.New("path traversal detected")
)

func ResolvePath(basePath, includePath string) string {
	resolved, _ := ResolvePathSafe(basePath, includePath)
	return resolved
}

func ResolvePathSafe(basePath, includePath string) (string, error) {
	if strings.HasPrefix(includePath, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if includePath == "~" {
				return home, nil
			}
			if len(includePath) > 1 && includePath[1] == '/' {
				includePath = filepath.Join(home, includePath[2:])
			}
		}
	}

	if filepath.IsAbs(includePath) {
		return filepath.Clean(includePath), nil
	}

	baseDir := filepath.Dir(basePath)
	resolved := filepath.Join(baseDir, includePath)
	resolved = filepath.Clean(resolved)

	if isSuspiciousPath(includePath) {
		return "", ErrPathTraversal
	}

	return resolved, nil
}

func isSuspiciousPath(path string) bool {
	clean := filepath.Clean(path)
	depth := 0
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." {
			depth++
		}
	}
	return depth > 5
}

func IsGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[") || strings.Contains(path, "<->")
}

func ConvertHledgerGlob(pattern string) string {
	return strings.ReplaceAll(pattern, "<->", "**")
}
