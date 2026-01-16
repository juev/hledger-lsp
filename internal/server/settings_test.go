package server

import (
	"testing"
	"time"

	"github.com/juev/hledger-lsp/internal/include"
)

func TestDefaultServerSettings(t *testing.T) {
	s := defaultServerSettings()

	if s.Completion.MaxResults != 50 {
		t.Errorf("Completion.MaxResults = %d, want 50", s.Completion.MaxResults)
	}
	if s.Limits.MaxFileSizeBytes != include.DefaultLimits().MaxFileSizeBytes {
		t.Errorf("Limits.MaxFileSizeBytes = %d, want %d", s.Limits.MaxFileSizeBytes, include.DefaultLimits().MaxFileSizeBytes)
	}

	// Features should default to true
	if !s.Features.Hover {
		t.Error("Features.Hover should default to true")
	}
	if !s.Features.Completion {
		t.Error("Features.Completion should default to true")
	}
	if !s.Features.Formatting {
		t.Error("Features.Formatting should default to true")
	}
	if !s.Features.Diagnostics {
		t.Error("Features.Diagnostics should default to true")
	}
	if !s.Features.SemanticTokens {
		t.Error("Features.SemanticTokens should default to true")
	}
	if !s.Features.CodeActions {
		t.Error("Features.CodeActions should default to true")
	}

	// Completion settings
	if !s.Completion.Snippets {
		t.Error("Completion.Snippets should default to true")
	}
	if !s.Completion.FuzzyMatching {
		t.Error("Completion.FuzzyMatching should default to true")
	}
	if !s.Completion.ShowCounts {
		t.Error("Completion.ShowCounts should default to true")
	}

	// Diagnostics settings
	if !s.Diagnostics.UndeclaredAccounts {
		t.Error("Diagnostics.UndeclaredAccounts should default to true")
	}
	if !s.Diagnostics.UndeclaredCommodities {
		t.Error("Diagnostics.UndeclaredCommodities should default to true")
	}
	if !s.Diagnostics.UnbalancedTransactions {
		t.Error("Diagnostics.UnbalancedTransactions should default to true")
	}

	// Formatting settings
	if s.Formatting.IndentSize != 4 {
		t.Errorf("Formatting.IndentSize = %d, want 4", s.Formatting.IndentSize)
	}
	if !s.Formatting.AlignAmounts {
		t.Error("Formatting.AlignAmounts should default to true")
	}

	// CLI settings
	if !s.CLI.Enabled {
		t.Error("CLI.Enabled should default to true")
	}
	if s.CLI.Path != "hledger" {
		t.Errorf("CLI.Path = %q, want %q", s.CLI.Path, "hledger")
	}
	if s.CLI.Timeout != 30*time.Second {
		t.Errorf("CLI.Timeout = %v, want %v", s.CLI.Timeout, 30*time.Second)
	}
}

func TestParseSettingsFromRaw_Features(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"features": map[string]interface{}{
			"hover":          false,
			"completion":     false,
			"formatting":     false,
			"diagnostics":    false,
			"semanticTokens": false,
			"codeActions":    false,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Features.Hover {
		t.Error("Features.Hover should be false")
	}
	if result.Features.Completion {
		t.Error("Features.Completion should be false")
	}
	if result.Features.Formatting {
		t.Error("Features.Formatting should be false")
	}
	if result.Features.Diagnostics {
		t.Error("Features.Diagnostics should be false")
	}
	if result.Features.SemanticTokens {
		t.Error("Features.SemanticTokens should be false")
	}
	if result.Features.CodeActions {
		t.Error("Features.CodeActions should be false")
	}
}

func TestParseSettingsFromRaw_Completion(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"completion": map[string]interface{}{
			"maxResults":    100,
			"snippets":      false,
			"fuzzyMatching": false,
			"showCounts":    false,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Completion.MaxResults != 100 {
		t.Errorf("Completion.MaxResults = %d, want 100", result.Completion.MaxResults)
	}
	if result.Completion.Snippets {
		t.Error("Completion.Snippets should be false")
	}
	if result.Completion.FuzzyMatching {
		t.Error("Completion.FuzzyMatching should be false")
	}
	if result.Completion.ShowCounts {
		t.Error("Completion.ShowCounts should be false")
	}
}

func TestParseSettingsFromRaw_Diagnostics(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"diagnostics": map[string]interface{}{
			"undeclaredAccounts":     false,
			"undeclaredCommodities":  false,
			"unbalancedTransactions": false,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Diagnostics.UndeclaredAccounts {
		t.Error("Diagnostics.UndeclaredAccounts should be false")
	}
	if result.Diagnostics.UndeclaredCommodities {
		t.Error("Diagnostics.UndeclaredCommodities should be false")
	}
	if result.Diagnostics.UnbalancedTransactions {
		t.Error("Diagnostics.UnbalancedTransactions should be false")
	}
}

func TestParseSettingsFromRaw_Formatting(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"formatting": map[string]interface{}{
			"indentSize":      2,
			"alignAmounts":    false,
			"alignmentColumn": 50,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Formatting.IndentSize != 2 {
		t.Errorf("Formatting.IndentSize = %d, want 2", result.Formatting.IndentSize)
	}
	if result.Formatting.AlignAmounts {
		t.Error("Formatting.AlignAmounts should be false")
	}
	if result.Formatting.AlignmentColumn != 50 {
		t.Errorf("Formatting.AlignmentColumn = %d, want 50", result.Formatting.AlignmentColumn)
	}
}

func TestParseSettingsFromRaw_CLI(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"cli": map[string]interface{}{
			"enabled": false,
			"path":    "/usr/local/bin/hledger",
			"timeout": 60000,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.CLI.Enabled {
		t.Error("CLI.Enabled should be false")
	}
	if result.CLI.Path != "/usr/local/bin/hledger" {
		t.Errorf("CLI.Path = %q, want %q", result.CLI.Path, "/usr/local/bin/hledger")
	}
	if result.CLI.Timeout != 60*time.Second {
		t.Errorf("CLI.Timeout = %v, want %v", result.CLI.Timeout, 60*time.Second)
	}
}

func TestParseSettingsFromRaw_FlatKeys(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"features.hover":                 false,
		"completion.snippets":            false,
		"diagnostics.undeclaredAccounts": false,
		"formatting.indentSize":          8,
		"formatting.alignmentColumn":     40,
		"cli.path":                       "/opt/hledger",
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Features.Hover {
		t.Error("Features.Hover should be false")
	}
	if result.Completion.Snippets {
		t.Error("Completion.Snippets should be false")
	}
	if result.Diagnostics.UndeclaredAccounts {
		t.Error("Diagnostics.UndeclaredAccounts should be false")
	}
	if result.Formatting.IndentSize != 8 {
		t.Errorf("Formatting.IndentSize = %d, want 8", result.Formatting.IndentSize)
	}
	if result.Formatting.AlignmentColumn != 40 {
		t.Errorf("Formatting.AlignmentColumn = %d, want 40", result.Formatting.AlignmentColumn)
	}
	if result.CLI.Path != "/opt/hledger" {
		t.Errorf("CLI.Path = %q, want %q", result.CLI.Path, "/opt/hledger")
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		input interface{}
		want  bool
		ok    bool
	}{
		{true, true, true},
		{false, false, true},
		{"true", true, true},
		{"false", false, true},
		{"TRUE", true, true},
		{"FALSE", false, true},
		{1, false, false},
		{nil, false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		got, ok := toBool(tt.input)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("toBool(%v) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
		ok    bool
	}{
		{"hello", "hello", true},
		{"", "", true},
		{123, "", false},
		{nil, "", false},
	}

	for _, tt := range tests {
		got, ok := toString(tt.input)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("toString(%v) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestServer_SetSettings_UpdatesCLI(t *testing.T) {
	srv := NewServer()

	settings := srv.getSettings()
	settings.CLI.Path = "/custom/path/hledger"
	settings.CLI.Timeout = 60 * time.Second
	srv.setSettings(settings)

	result := srv.getSettings()
	if result.CLI.Path != "/custom/path/hledger" {
		t.Errorf("CLI.Path = %q, want %q", result.CLI.Path, "/custom/path/hledger")
	}
	if result.CLI.Timeout != 60*time.Second {
		t.Errorf("CLI.Timeout = %v, want %v", result.CLI.Timeout, 60*time.Second)
	}
}
