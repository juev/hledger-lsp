package include

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath_RelativePath(t *testing.T) {
	tests := []struct {
		name        string
		basePath    string
		includePath string
		want        string
	}{
		{
			name:        "simple relative path",
			basePath:    "/home/user/finances/main.journal",
			includePath: "accounts.journal",
			want:        "/home/user/finances/accounts.journal",
		},
		{
			name:        "relative path with subdirectory",
			basePath:    "/home/user/finances/main.journal",
			includePath: "2024/january.journal",
			want:        "/home/user/finances/2024/january.journal",
		},
		{
			name:        "relative path with parent directory",
			basePath:    "/home/user/finances/2024/main.journal",
			includePath: "../accounts.journal",
			want:        "/home/user/finances/accounts.journal",
		},
		{
			name:        "relative path with current directory",
			basePath:    "/home/user/finances/main.journal",
			includePath: "./accounts.journal",
			want:        "/home/user/finances/accounts.journal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.basePath, tt.includePath)
			if got != tt.want {
				t.Errorf("ResolvePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePath_AbsolutePath(t *testing.T) {
	tests := []struct {
		name        string
		basePath    string
		includePath string
		want        string
	}{
		{
			name:        "absolute path",
			basePath:    "/home/user/finances/main.journal",
			includePath: "/etc/hledger/accounts.journal",
			want:        "/etc/hledger/accounts.journal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.basePath, tt.includePath)
			if got != tt.want {
				t.Errorf("ResolvePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePath_HomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get home directory")
	}

	tests := []struct {
		name        string
		basePath    string
		includePath string
		want        string
	}{
		{
			name:        "tilde expands to home",
			basePath:    "/some/other/path/main.journal",
			includePath: "~/finances/accounts.journal",
			want:        filepath.Join(home, "finances/accounts.journal"),
		},
		{
			name:        "just tilde",
			basePath:    "/some/path/main.journal",
			includePath: "~",
			want:        home,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.basePath, tt.includePath)
			if got != tt.want {
				t.Errorf("ResolvePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePath_Normalization(t *testing.T) {
	tests := []struct {
		name        string
		basePath    string
		includePath string
		want        string
	}{
		{
			name:        "multiple parent directories",
			basePath:    "/home/user/finances/2024/q1/main.journal",
			includePath: "../../accounts.journal",
			want:        "/home/user/finances/accounts.journal",
		},
		{
			name:        "mixed . and ..",
			basePath:    "/home/user/finances/main.journal",
			includePath: "./2024/../accounts.journal",
			want:        "/home/user/finances/accounts.journal",
		},
		{
			name:        "double slashes normalized",
			basePath:    "/home/user//finances/main.journal",
			includePath: "accounts.journal",
			want:        "/home/user/finances/accounts.journal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.basePath, tt.includePath)
			if got != tt.want {
				t.Errorf("ResolvePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePath_GlobPatterns(t *testing.T) {
	tests := []struct {
		name        string
		basePath    string
		includePath string
		want        string
	}{
		{
			name:        "wildcard pattern preserved",
			basePath:    "/home/user/finances/main.journal",
			includePath: "2024/*.journal",
			want:        "/home/user/finances/2024/*.journal",
		},
		{
			name:        "recursive glob pattern",
			basePath:    "/home/user/finances/main.journal",
			includePath: "**/*.journal",
			want:        "/home/user/finances/**/*.journal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePath(tt.basePath, tt.includePath)
			if got != tt.want {
				t.Errorf("ResolvePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
