package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/fmtcmd"
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
	// InlineCompletion should default to true
	if !s.Features.InlineCompletion {
		t.Error("Features.InlineCompletion should default to true")
	}
	// CodeLens should default to false
	if s.Features.CodeLens {
		t.Error("Features.CodeLens should default to false")
	}

	// Completion settings
	if !s.Completion.FuzzyMatching {
		t.Error("Completion.FuzzyMatching should default to true")
	}
	if !s.Completion.ShowCounts {
		t.Error("Completion.ShowCounts should default to true")
	}
	if !s.Completion.IncludeNotes {
		t.Error("Completion.IncludeNotes should default to true")
	}

	// Diagnostics settings. The defaults follow hledger: account declarations
	// are only required under --strict, and commodity directives are optional.
	if s.Diagnostics.AccountCheck != accountCheckOff {
		t.Errorf("Diagnostics.AccountCheck = %q, want %q", s.Diagnostics.AccountCheck, accountCheckOff)
	}
	if s.Diagnostics.UndeclaredCommodities {
		t.Error("Diagnostics.UndeclaredCommodities should default to false")
	}
	if !s.Diagnostics.UnbalancedTransactions {
		t.Error("Diagnostics.UnbalancedTransactions should default to true")
	}
	if !s.Diagnostics.BalanceAssertions {
		t.Error("Diagnostics.BalanceAssertions should default to true")
	}
	if s.Diagnostics.DebounceMs != 100 {
		t.Errorf("Diagnostics.DebounceMs = %d, want 100", s.Diagnostics.DebounceMs)
	}

	// Formatting settings
	if s.Formatting.IndentSize != 4 {
		t.Errorf("Formatting.IndentSize = %d, want 4", s.Formatting.IndentSize)
	}
	if !s.Formatting.AlignAmounts {
		t.Error("Formatting.AlignAmounts should default to true")
	}
	if s.Formatting.MinAlignmentColumn != 0 {
		t.Errorf("Formatting.MinAlignmentColumn = %d, want 0 (auto from file content)", s.Formatting.MinAlignmentColumn)
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
			"hover":            false,
			"completion":       false,
			"formatting":       false,
			"diagnostics":      false,
			"semanticTokens":   false,
			"codeActions":      false,
			"inlineCompletion": true,
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
	if !result.Features.InlineCompletion {
		t.Error("Features.InlineCompletion should be true")
	}
}

func TestParseSettingsFromRaw_Completion(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"completion": map[string]interface{}{
			"maxResults":    100,
			"fuzzyMatching": false,
			"showCounts":    false,
			"includeNotes":  false,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Completion.MaxResults != 100 {
		t.Errorf("Completion.MaxResults = %d, want 100", result.Completion.MaxResults)
	}
	if result.Completion.FuzzyMatching {
		t.Error("Completion.FuzzyMatching should be false")
	}
	if result.Completion.ShowCounts {
		t.Error("Completion.ShowCounts should be false")
	}
	if result.Completion.IncludeNotes {
		t.Error("Completion.IncludeNotes should be false")
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

	if result.Diagnostics.AccountCheck != accountCheckOff {
		t.Errorf("Diagnostics.AccountCheck = %q, want %q (legacy false disables the check)", result.Diagnostics.AccountCheck, accountCheckOff)
	}
	if result.Diagnostics.UndeclaredCommodities {
		t.Error("Diagnostics.UndeclaredCommodities should be false")
	}
	if result.Diagnostics.UnbalancedTransactions {
		t.Error("Diagnostics.UnbalancedTransactions should be false")
	}
}

func TestParseSettingsFromRaw_AccountCheck(t *testing.T) {
	base := defaultServerSettings()

	t.Run("explicit mode wins over the legacy flag", func(t *testing.T) {
		raw := map[string]interface{}{
			"diagnostics": map[string]interface{}{
				"accountCheck":       "strict",
				"undeclaredAccounts": true,
			},
		}
		assert.Equal(t, accountCheckStrict, parseSettingsFromRaw(base, raw).Diagnostics.AccountCheck)
	})

	t.Run("legacy true maps to lint", func(t *testing.T) {
		raw := map[string]interface{}{
			"diagnostics": map[string]interface{}{"undeclaredAccounts": true},
		}
		assert.Equal(t, accountCheckLint, parseSettingsFromRaw(base, raw).Diagnostics.AccountCheck)
	})

	t.Run("dotted key", func(t *testing.T) {
		raw := map[string]interface{}{"diagnostics.accountCheck": "lint"}
		assert.Equal(t, accountCheckLint, parseSettingsFromRaw(base, raw).Diagnostics.AccountCheck)
	})

	t.Run("unknown value falls back to the default", func(t *testing.T) {
		raw := map[string]interface{}{
			"diagnostics": map[string]interface{}{"accountCheck": "sometimes"},
		}
		assert.Equal(t, accountCheckOff, parseSettingsFromRaw(base, raw).Diagnostics.AccountCheck)
	})

	t.Run("balance assertions and debounce", func(t *testing.T) {
		raw := map[string]interface{}{
			"diagnostics": map[string]interface{}{
				"balanceAssertions": false,
				"debounceMs":        250,
			},
		}
		result := parseSettingsFromRaw(base, raw)
		assert.False(t, result.Diagnostics.BalanceAssertions)
		assert.Equal(t, 250, result.Diagnostics.DebounceMs)
	})
}

func TestParseSettingsFromRaw_Formatting(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"formatting": map[string]interface{}{
			"indentSize":         2,
			"alignAmounts":       false,
			"minAlignmentColumn": 50,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Formatting.IndentSize != 2 {
		t.Errorf("Formatting.IndentSize = %d, want 2", result.Formatting.IndentSize)
	}
	if result.Formatting.AlignAmounts {
		t.Error("Formatting.AlignAmounts should be false")
	}
	if result.Formatting.MinAlignmentColumn != 50 {
		t.Errorf("Formatting.MinAlignmentColumn = %d, want 50", result.Formatting.MinAlignmentColumn)
	}
}

func TestParseSettingsFromRaw_Formatting_AmountAlignmentColumn(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"formatting": map[string]interface{}{
			"indentSize":            2,
			"alignAmounts":          true,
			"amountAlignmentColumn": 45,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Formatting.IndentSize != 2 {
		t.Errorf("Formatting.IndentSize = %d, want 2", result.Formatting.IndentSize)
	}
	if !result.Formatting.AlignAmounts {
		t.Error("Formatting.AlignAmounts should be true")
	}
	if result.Formatting.AmountAlignmentColumn != 45 {
		t.Errorf("Formatting.AmountAlignmentColumn = %d, want 45", result.Formatting.AmountAlignmentColumn)
	}
	if result.Formatting.MinAlignmentColumn != 0 {
		t.Errorf("Formatting.MinAlignmentColumn = %d, want 0 when amountAlignmentColumn is used", result.Formatting.MinAlignmentColumn)
	}
}

func TestParseSettingsFromRaw_Formatting_FlatAmountAlignmentColumn(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"formatting.indentSize":            8,
		"formatting.amountAlignmentColumn": 42,
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Formatting.IndentSize != 8 {
		t.Errorf("Formatting.IndentSize = %d, want 8", result.Formatting.IndentSize)
	}
	if result.Formatting.AmountAlignmentColumn != 42 {
		t.Errorf("Formatting.AmountAlignmentColumn = %d, want 42", result.Formatting.AmountAlignmentColumn)
	}
	if result.Formatting.MinAlignmentColumn != 0 {
		t.Errorf("Formatting.MinAlignmentColumn = %d, want 0 when flat amountAlignmentColumn is used", result.Formatting.MinAlignmentColumn)
	}
}

func TestParseSettingsFromRaw_Formatting_AmountAlignmentTarget(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		result := parseSettingsFromRaw(defaultServerSettings(), map[string]interface{}{
			"formatting": map[string]interface{}{
				"amountAlignmentTarget": "posting",
			},
		})
		if result.Formatting.AmountAlignmentTarget != "posting" {
			t.Errorf("AmountAlignmentTarget = %q, want \"posting\"", result.Formatting.AmountAlignmentTarget)
		}
	})

	t.Run("flat", func(t *testing.T) {
		result := parseSettingsFromRaw(defaultServerSettings(), map[string]interface{}{
			"formatting.amountAlignmentTarget": "posting",
		})
		if result.Formatting.AmountAlignmentTarget != "posting" {
			t.Errorf("AmountAlignmentTarget = %q, want \"posting\"", result.Formatting.AmountAlignmentTarget)
		}
	})

	t.Run("absent leaves empty", func(t *testing.T) {
		result := parseSettingsFromRaw(defaultServerSettings(), map[string]interface{}{
			"formatting": map[string]interface{}{"indentSize": 4},
		})
		if result.Formatting.AmountAlignmentTarget != "" {
			t.Errorf("AmountAlignmentTarget = %q, want empty", result.Formatting.AmountAlignmentTarget)
		}
	})
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
		"features.inlineCompletion":      true,
		"diagnostics.undeclaredAccounts": false,
		"formatting.indentSize":          8,
		"formatting.minAlignmentColumn":  40,
		"cli.path":                       "/opt/hledger",
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Features.Hover {
		t.Error("Features.Hover should be false")
	}
	if !result.Features.InlineCompletion {
		t.Error("Features.InlineCompletion should be true")
	}
	if result.Diagnostics.AccountCheck != accountCheckOff {
		t.Errorf("Diagnostics.AccountCheck = %q, want %q", result.Diagnostics.AccountCheck, accountCheckOff)
	}
	if result.Formatting.IndentSize != 8 {
		t.Errorf("Formatting.IndentSize = %d, want 8", result.Formatting.IndentSize)
	}
	if result.Formatting.MinAlignmentColumn != 40 {
		t.Errorf("Formatting.MinAlignmentColumn = %d, want 40", result.Formatting.MinAlignmentColumn)
	}
	if result.CLI.Path != "/opt/hledger" {
		t.Errorf("CLI.Path = %q, want %q", result.CLI.Path, "/opt/hledger")
	}
}

func TestParseSettingsFromRaw_InlayHints(t *testing.T) {
	base := defaultServerSettings()
	if !base.Features.InlayHints || !base.InlayHints.InferredAmounts || base.InlayHints.RunningBalances || base.InlayHints.CostExpansion {
		t.Fatal("unexpected inlay hint defaults")
	}

	result := parseSettingsFromRaw(base, map[string]interface{}{
		"features": map[string]interface{}{"inlayHints": false},
		"inlayHints": map[string]interface{}{
			"inferredAmounts": false,
			"runningBalances": true,
			"costExpansion":   true,
		},
	})
	if result.Features.InlayHints || result.InlayHints.InferredAmounts || !result.InlayHints.RunningBalances || !result.InlayHints.CostExpansion {
		t.Fatal("nested inlay hint settings were not applied")
	}

	result = parseSettingsFromRaw(base, map[string]interface{}{
		"features.inlayHints":        false,
		"inlayHints.inferredAmounts": false,
		"inlayHints.runningBalances": true,
		"inlayHints.costExpansion":   true,
	})
	if result.Features.InlayHints || result.InlayHints.InferredAmounts || !result.InlayHints.RunningBalances || !result.InlayHints.CostExpansion {
		t.Fatal("dotted inlay hint settings were not applied")
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

func TestServer_SetSettings_ClearsAlignmentCacheOnFormattingChange(t *testing.T) {
	srv := NewServer()
	uri := "file:///test.journal"
	srv.alignmentCache.Store(uri, 42)

	settings := srv.getSettings()
	settings.Formatting.MinAlignmentColumn = 50
	srv.setSettings(settings)

	_, loaded := srv.alignmentCache.Load(uri)
	if loaded {
		t.Error("alignmentCache should be cleared when formatting settings change")
	}
}

func TestParseSettingsFromRaw_Diagnostics_BalanceTolerance(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"diagnostics": map[string]interface{}{
			"balanceTolerance": 0.01,
		},
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Diagnostics.BalanceTolerance != 0.01 {
		t.Errorf("Diagnostics.BalanceTolerance = %v, want 0.01", result.Diagnostics.BalanceTolerance)
	}
}

func TestParseSettingsFromRaw_Diagnostics_BalanceTolerance_FlatKey(t *testing.T) {
	base := defaultServerSettings()

	raw := map[string]interface{}{
		"diagnostics.balanceTolerance": 0.005,
	}

	result := parseSettingsFromRaw(base, raw)

	if result.Diagnostics.BalanceTolerance != 0.005 {
		t.Errorf("Diagnostics.BalanceTolerance = %v, want 0.005", result.Diagnostics.BalanceTolerance)
	}
}

func TestDefaultServerSettings_BalanceToleranceZero(t *testing.T) {
	s := defaultServerSettings()

	if s.Diagnostics.BalanceTolerance != 0 {
		t.Errorf("Diagnostics.BalanceTolerance = %v, want 0", s.Diagnostics.BalanceTolerance)
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input interface{}
		want  float64
		ok    bool
	}{
		{float64(0.01), 0.01, true},
		{float32(0.5), 0.5, true},
		{int(1), 1.0, true},
		{int64(2), 2.0, true},
		{"0.01", 0.01, true},
		{"abc", 0, false},
		{nil, 0, false},
		{true, 0, false},
	}

	for _, tt := range tests {
		got, ok := toFloat64(tt.input)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("toFloat64(%v) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDefaultServerSettings_AmountAlignmentMode(t *testing.T) {
	s := defaultServerSettings()
	if s.Formatting.AmountAlignmentMode != "right" {
		t.Errorf("Formatting.AmountAlignmentMode = %q, want %q", s.Formatting.AmountAlignmentMode, "right")
	}
}

func TestParseSettingsFromRaw_Formatting_AmountAlignmentMode(t *testing.T) {
	base := defaultServerSettings()

	t.Run("nested decimal", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting": map[string]interface{}{
				"amountAlignmentMode": "decimal",
			},
		}
		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "decimal" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "decimal")
		}
	})

	t.Run("nested right", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting": map[string]interface{}{
				"amountAlignmentMode": "right",
			},
		}
		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "right" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "right")
		}
	})

	t.Run("nested left", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting": map[string]interface{}{
				"amountAlignmentMode": "left",
			},
		}
		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "left" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "left")
		}
	})

	t.Run("invalid value falls back to right", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting": map[string]interface{}{
				"amountAlignmentMode": "invalid",
			},
		}
		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "right" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "right")
		}
	})

	t.Run("absent uses default", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting": map[string]interface{}{
				"indentSize": 2,
			},
		}
		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "right" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "right")
		}
	})
}

func TestParseSettingsFromRaw_Formatting_AmountAlignmentMode_FlatKey(t *testing.T) {
	base := defaultServerSettings()

	t.Run("decimal", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting.amountAlignmentMode": "decimal",
		}

		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "decimal" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "decimal")
		}
	})

	t.Run("left", func(t *testing.T) {
		raw := map[string]interface{}{
			"formatting.amountAlignmentMode": "left",
		}

		result := parseSettingsFromRaw(base, raw)
		if result.Formatting.AmountAlignmentMode != "left" {
			t.Errorf("AmountAlignmentMode = %q, want %q", result.Formatting.AmountAlignmentMode, "left")
		}
	})
}

func TestServer_SetSettings_ClearsAlignmentCacheOnAlignmentModeChange(t *testing.T) {
	srv := NewServer()
	uri := "file:///test.journal"
	srv.alignmentCache.Store(uri, 42)

	settings := srv.getSettings()
	settings.Formatting.AmountAlignmentMode = "decimal"
	srv.setSettings(settings)

	_, loaded := srv.alignmentCache.Load(uri)
	if loaded {
		t.Error("alignmentCache should be cleared when AmountAlignmentMode changes")
	}
}

func TestServer_SetSettings_KeepsAlignmentCacheWhenFormattingUnchanged(t *testing.T) {
	srv := NewServer()
	uri := "file:///test.journal"
	srv.alignmentCache.Store(uri, 42)

	settings := srv.getSettings()
	settings.CLI.Path = "/other/hledger"
	srv.setSettings(settings)

	val, loaded := srv.alignmentCache.Load(uri)
	if !loaded || val.(int) != 42 {
		t.Error("alignmentCache should be preserved when formatting settings don't change")
	}
}

func TestSettings_LegacyUndeclaredAccountsFalseDisablesTheCheck(t *testing.T) {
	base := defaultServerSettings()
	base.Diagnostics.AccountCheck = accountCheckLint

	// A refresh parses on top of the live settings, so the legacy boolean must
	// map both ways: false has to turn the check off again.
	settings := parseSettingsFromRaw(base, map[string]interface{}{
		"diagnostics": map[string]interface{}{"undeclaredAccounts": false},
	})
	assert.Equal(t, accountCheckOff, settings.Diagnostics.AccountCheck)

	enabled := parseSettingsFromRaw(defaultServerSettings(), map[string]interface{}{
		"diagnostics": map[string]interface{}{"undeclaredAccounts": true},
	})
	assert.Equal(t, accountCheckLint, enabled.Diagnostics.AccountCheck)
}

func TestFormatterDefaultsMatchHledgerFmt(t *testing.T) {
	if got, want := formatterOptionsFrom(defaultServerSettings().Formatting), fmtcmd.DefaultOptions(); got != want {
		t.Errorf("server defaults %+v differ from hledger-fmt defaults %+v", got, want)
	}
}

// configClient answers workspace/configuration with fixed settings and counts
// workspace/inlayHint/refresh requests.
type configClient struct {
	protocol.UnimplementedClient
	settings  protocol.LSPAny
	refreshes atomic.Int32
}

func (c *configClient) Configuration(context.Context, *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	return []protocol.LSPAny{c.settings}, nil
}

func (c *configClient) InlayHintRefresh(context.Context) error {
	c.refreshes.Add(1)
	return nil
}

// The client asks for the hints of the first buffer before the server has
// fetched its configuration, so a setting that changes the hints must make the
// client ask again, or they stay as the defaults made them until another
// request comes along.
func TestRefreshConfiguration_RefreshesInlayHintsWhenTheirSettingsChange(t *testing.T) {
	enabled := mustMarshalLSPAny(t, map[string]any{"inlayHints": map[string]any{"runningBalances": true}})
	refreshSupport := &protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{
		Configuration: boolPtr(true),
		InlayHint:     &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: boolPtr(true)},
	}}

	t.Run("changed", func(t *testing.T) {
		srv := NewServer()
		_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: *refreshSupport})
		require.NoError(t, err)
		client := &configClient{settings: enabled}
		srv.SetClient(client)

		srv.refreshConfiguration(context.Background())
		assert.True(t, srv.getSettings().InlayHints.RunningBalances)
		assert.Equal(t, int32(1), client.refreshes.Load())

		srv.refreshConfiguration(context.Background())
		assert.Equal(t, int32(1), client.refreshes.Load(), "unchanged hint settings need no refresh")
	})

	t.Run("client without refresh support", func(t *testing.T) {
		srv := NewServer()
		capabilities := *refreshSupport
		capabilities.Workspace = &protocol.WorkspaceClientCapabilities{Configuration: boolPtr(true)}
		_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: capabilities})
		require.NoError(t, err)
		client := &configClient{settings: enabled}
		srv.SetClient(client)

		srv.refreshConfiguration(context.Background())
		assert.True(t, srv.getSettings().InlayHints.RunningBalances)
		assert.Zero(t, client.refreshes.Load())
	})
}

// heldConfigClient answers workspace/configuration only once release is closed.
type heldConfigClient struct {
	configClient
	release chan struct{}
}

func (c *heldConfigClient) Configuration(ctx context.Context, params *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	<-c.release
	return c.configClient.Configuration(ctx, params)
}

// The hints of the first buffer are asked for before the configuration arrives.
// Answered with the defaults they would leave out the running balances the
// configuration turns on, and Neovim keeps showing those stale hints on the
// visible lines even after a refresh, so the request waits for the settings.
func TestInlayHint_WaitsForTheInitialConfiguration(t *testing.T) {
	srv := NewServer()
	_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
		Workspace: &protocol.WorkspaceClientCapabilities{Configuration: boolPtr(true)},
	}})
	require.NoError(t, err)
	client := &heldConfigClient{
		configClient: configClient{settings: mustMarshalLSPAny(t, map[string]any{"inlayHints": map[string]any{"runningBalances": true}})},
		release:      make(chan struct{}),
	}
	srv.SetClient(client)
	require.NoError(t, srv.Initialized(context.Background(), &protocol.InitializedParams{}))

	docURI := uri.URI("file:///test.journal")
	srv.StoreDocument(docURI, "2024-01-15 Покупка\n    expenses:еда  $50\n    assets:cash\n")
	answered := make(chan []protocol.InlayHint)
	go func() {
		hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Range:        protocol.Range{End: protocol.Position{Line: 10}},
		})
		assert.NoError(t, err)
		answered <- hints
	}()

	select {
	case <-answered:
		t.Fatal("inlay hints were answered before the configuration arrived")
	case <-time.After(50 * time.Millisecond):
	}
	close(client.release)

	var labels []protocol.InlayHintLabel
	for _, hint := range <-answered {
		labels = append(labels, hint.Label)
	}
	assert.Contains(t, labels, protocol.String("balance: 50 $"))
}

// gatedRefreshClient answers workspace/configuration with fixed settings, or
// with configErr, and holds workspace/inlayHint/refresh until release is closed.
type gatedRefreshClient struct {
	protocol.UnimplementedClient
	settings  []protocol.LSPAny
	configErr error
	refreshed chan struct{}
	release   chan struct{}
}

func (c *gatedRefreshClient) Configuration(context.Context, *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	return c.settings, c.configErr
}

func (c *gatedRefreshClient) InlayHintRefresh(context.Context) error {
	close(c.refreshed)
	<-c.release
	return nil
}

// A client may ask for the hints again before it answers the refresh, so the
// requests waiting for the initial configuration are released as soon as the
// settings are applied, whatever happens to the refresh.
func TestInitialized_ConfigurationReadyBeforeInlayHintRefreshIsAnswered(t *testing.T) {
	enabled := mustMarshalLSPAny(t, map[string]any{"inlayHints": map[string]any{"runningBalances": true}})
	cases := []struct {
		name          string
		configuration bool
		settings      []protocol.LSPAny
		configErr     error
		refresh       bool
	}{
		{name: "refresh held", configuration: true, settings: []protocol.LSPAny{enabled}, refresh: true},
		{name: "unchanged hint settings", configuration: true, settings: []protocol.LSPAny{mustMarshalLSPAny(t, map[string]any{})}},
		{name: "configuration error", configuration: true, configErr: errors.New("configuration unavailable")},
		{name: "empty configuration", configuration: true},
		{name: "no configuration support"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer()
			_, err := srv.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{
				Workspace: &protocol.WorkspaceClientCapabilities{
					Configuration: boolPtr(tc.configuration),
					InlayHint:     &protocol.InlayHintWorkspaceClientCapabilities{RefreshSupport: boolPtr(true)},
				},
			}})
			require.NoError(t, err)
			client := &gatedRefreshClient{
				settings: tc.settings, configErr: tc.configErr,
				refreshed: make(chan struct{}), release: make(chan struct{}),
			}
			defer close(client.release)
			srv.SetClient(client)
			docURI := uri.URI("file:///ready.journal")
			srv.StoreDocument(docURI, "2024-01-15 Покупка\n    expenses:еда  $50\n    assets:cash\n")
			require.NoError(t, srv.Initialized(context.Background(), &protocol.InitializedParams{}))

			srv.settingsMu.RLock()
			ready := srv.configReady
			srv.settingsMu.RUnlock()
			switch {
			case !tc.configuration:
				assert.Nil(t, ready, "nothing is fetched, so nothing is waited for")
			case tc.refresh:
				select {
				case <-client.refreshed:
				case <-time.After(5 * time.Second):
					t.Fatal("the changed hint settings did not ask the client to refresh")
				}
				// The refresh is sent after the settings are applied, so they
				// must already be ready while the client holds its reply.
				select {
				case <-ready:
				default:
					t.Fatal("hint requests still wait for the configuration while the refresh is unanswered")
				}
			default:
				select {
				case <-ready:
				case <-time.After(5 * time.Second):
					t.Fatal("the finished configuration fetch did not release waiting requests")
				}
			}

			hints, err := srv.InlayHint(context.Background(), &protocol.InlayHintParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
				Range:        protocol.Range{End: protocol.Position{Line: 10}},
			})
			require.NoError(t, err)
			var labels []protocol.InlayHintLabel
			for _, hint := range hints {
				labels = append(labels, hint.Label)
			}
			if tc.refresh {
				assert.Contains(t, labels, protocol.String("balance: 50 $"))
			} else {
				assert.NotContains(t, labels, protocol.String("balance: 50 $"))
			}
		})
	}
}
