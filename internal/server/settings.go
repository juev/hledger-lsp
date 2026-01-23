package server

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/include"
)

type featureSettings struct {
	Hover            bool
	Completion       bool
	Formatting       bool
	Diagnostics      bool
	SemanticTokens   bool
	CodeActions      bool
	FoldingRanges    bool
	DocumentLinks    bool
	WorkspaceSymbol  bool
	InlineCompletion bool
}

type completionSettings struct {
	MaxResults    int
	Snippets      bool
	FuzzyMatching bool
	ShowCounts    bool
}

type diagnosticsSettings struct {
	UndeclaredAccounts     bool
	UndeclaredCommodities  bool
	UnbalancedTransactions bool
}

type formattingSettings struct {
	IndentSize         int
	AlignAmounts       bool
	MinAlignmentColumn int
}

type cliSettings struct {
	Enabled bool
	Path    string
	Timeout time.Duration
}

type serverSettings struct {
	Features    featureSettings
	Completion  completionSettings
	Diagnostics diagnosticsSettings
	Formatting  formattingSettings
	CLI         cliSettings
	Limits      include.Limits
}

func defaultServerSettings() serverSettings {
	return serverSettings{
		Features: featureSettings{
			Hover:            true,
			Completion:       true,
			Formatting:       true,
			Diagnostics:      true,
			SemanticTokens:   true,
			CodeActions:      true,
			FoldingRanges:    true,
			DocumentLinks:    true,
			WorkspaceSymbol:  true,
			InlineCompletion: false,
		},
		Completion: completionSettings{
			MaxResults:    50,
			Snippets:      true,
			FuzzyMatching: true,
			ShowCounts:    true,
		},
		Diagnostics: diagnosticsSettings{
			UndeclaredAccounts:     true,
			UndeclaredCommodities:  true,
			UnbalancedTransactions: true,
		},
		Formatting: formattingSettings{
			IndentSize:   4,
			AlignAmounts: true,
		},
		CLI: cliSettings{
			Enabled: true,
			Path:    "hledger",
			Timeout: 30 * time.Second,
		},
		Limits: include.DefaultLimits(),
	}
}

func normalizeServerSettings(settings serverSettings) serverSettings {
	defaults := defaultServerSettings()
	if settings.Completion.MaxResults <= 0 {
		settings.Completion.MaxResults = defaults.Completion.MaxResults
	}
	if settings.Formatting.IndentSize <= 0 {
		settings.Formatting.IndentSize = defaults.Formatting.IndentSize
	}
	if settings.CLI.Path == "" {
		settings.CLI.Path = defaults.CLI.Path
	}
	if settings.CLI.Timeout <= 0 {
		settings.CLI.Timeout = defaults.CLI.Timeout
	}
	if settings.Limits.MaxFileSizeBytes <= 0 {
		settings.Limits.MaxFileSizeBytes = defaults.Limits.MaxFileSizeBytes
	}
	if settings.Limits.MaxIncludeDepth <= 0 {
		settings.Limits.MaxIncludeDepth = defaults.Limits.MaxIncludeDepth
	}
	return settings
}

func (s *Server) setSettings(settings serverSettings) {
	settings = normalizeServerSettings(settings)
	s.settingsMu.Lock()
	oldSettings := s.settings
	s.settings = settings
	s.settingsMu.Unlock()
	if s.loader != nil {
		s.loader.SetLimits(settings.Limits)
	}
	if oldSettings.CLI.Path != settings.CLI.Path || oldSettings.CLI.Timeout != settings.CLI.Timeout {
		s.reinitCLI(settings.CLI)
	}
}

func (s *Server) getSettings() serverSettings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings
}

func (s *Server) refreshConfiguration(ctx context.Context) {
	if s.client == nil || !s.supportsConfiguration {
		return
	}
	result, err := s.client.Configuration(ctx, &protocol.ConfigurationParams{
		Items: []protocol.ConfigurationItem{
			{Section: "hledger"},
		},
	})
	if err != nil || len(result) == 0 {
		return
	}
	settings := parseSettingsFromRaw(s.getSettings(), result[0])
	s.setSettings(settings)
}

func (s *Server) DidChangeConfiguration(_ context.Context, _ *protocol.DidChangeConfigurationParams) error {
	go s.refreshConfiguration(context.Background())
	return nil
}

func parseSettingsFromRaw(base serverSettings, raw interface{}) serverSettings {
	settings := base
	rawMap, ok := raw.(map[string]interface{})
	if !ok {
		return normalizeServerSettings(settings)
	}
	if nested, ok := rawMap["hledger"]; ok {
		return parseSettingsFromRaw(settings, nested)
	}
	settings = applySettingsMap(settings, rawMap)
	return normalizeServerSettings(settings)
}

func applySettingsMap(settings serverSettings, raw map[string]interface{}) serverSettings {
	// Features
	if featuresRaw, ok := raw["features"].(map[string]interface{}); ok {
		if value, ok := toBool(featuresRaw["hover"]); ok {
			settings.Features.Hover = value
		}
		if value, ok := toBool(featuresRaw["completion"]); ok {
			settings.Features.Completion = value
		}
		if value, ok := toBool(featuresRaw["formatting"]); ok {
			settings.Features.Formatting = value
		}
		if value, ok := toBool(featuresRaw["diagnostics"]); ok {
			settings.Features.Diagnostics = value
		}
		if value, ok := toBool(featuresRaw["semanticTokens"]); ok {
			settings.Features.SemanticTokens = value
		}
		if value, ok := toBool(featuresRaw["codeActions"]); ok {
			settings.Features.CodeActions = value
		}
		if value, ok := toBool(featuresRaw["foldingRanges"]); ok {
			settings.Features.FoldingRanges = value
		}
		if value, ok := toBool(featuresRaw["documentLinks"]); ok {
			settings.Features.DocumentLinks = value
		}
		if value, ok := toBool(featuresRaw["workspaceSymbol"]); ok {
			settings.Features.WorkspaceSymbol = value
		}
		if value, ok := toBool(featuresRaw["inlineCompletion"]); ok {
			settings.Features.InlineCompletion = value
		}
	}
	if value, ok := toBool(raw["features.hover"]); ok {
		settings.Features.Hover = value
	}
	if value, ok := toBool(raw["features.completion"]); ok {
		settings.Features.Completion = value
	}
	if value, ok := toBool(raw["features.formatting"]); ok {
		settings.Features.Formatting = value
	}
	if value, ok := toBool(raw["features.diagnostics"]); ok {
		settings.Features.Diagnostics = value
	}
	if value, ok := toBool(raw["features.semanticTokens"]); ok {
		settings.Features.SemanticTokens = value
	}
	if value, ok := toBool(raw["features.codeActions"]); ok {
		settings.Features.CodeActions = value
	}
	if value, ok := toBool(raw["features.foldingRanges"]); ok {
		settings.Features.FoldingRanges = value
	}
	if value, ok := toBool(raw["features.documentLinks"]); ok {
		settings.Features.DocumentLinks = value
	}
	if value, ok := toBool(raw["features.workspaceSymbol"]); ok {
		settings.Features.WorkspaceSymbol = value
	}
	if value, ok := toBool(raw["features.inlineCompletion"]); ok {
		settings.Features.InlineCompletion = value
	}

	// Completion
	if completionRaw, ok := raw["completion"].(map[string]interface{}); ok {
		if value, ok := toInt(completionRaw["maxResults"]); ok {
			settings.Completion.MaxResults = value
		}
		if value, ok := toBool(completionRaw["snippets"]); ok {
			settings.Completion.Snippets = value
		}
		if value, ok := toBool(completionRaw["fuzzyMatching"]); ok {
			settings.Completion.FuzzyMatching = value
		}
		if value, ok := toBool(completionRaw["showCounts"]); ok {
			settings.Completion.ShowCounts = value
		}
	}
	if value, ok := toInt(raw["completion.maxResults"]); ok {
		settings.Completion.MaxResults = value
	}
	if value, ok := toBool(raw["completion.snippets"]); ok {
		settings.Completion.Snippets = value
	}
	if value, ok := toBool(raw["completion.fuzzyMatching"]); ok {
		settings.Completion.FuzzyMatching = value
	}
	if value, ok := toBool(raw["completion.showCounts"]); ok {
		settings.Completion.ShowCounts = value
	}

	// Diagnostics
	if diagnosticsRaw, ok := raw["diagnostics"].(map[string]interface{}); ok {
		if value, ok := toBool(diagnosticsRaw["undeclaredAccounts"]); ok {
			settings.Diagnostics.UndeclaredAccounts = value
		}
		if value, ok := toBool(diagnosticsRaw["undeclaredCommodities"]); ok {
			settings.Diagnostics.UndeclaredCommodities = value
		}
		if value, ok := toBool(diagnosticsRaw["unbalancedTransactions"]); ok {
			settings.Diagnostics.UnbalancedTransactions = value
		}
	}
	if value, ok := toBool(raw["diagnostics.undeclaredAccounts"]); ok {
		settings.Diagnostics.UndeclaredAccounts = value
	}
	if value, ok := toBool(raw["diagnostics.undeclaredCommodities"]); ok {
		settings.Diagnostics.UndeclaredCommodities = value
	}
	if value, ok := toBool(raw["diagnostics.unbalancedTransactions"]); ok {
		settings.Diagnostics.UnbalancedTransactions = value
	}

	// Formatting
	if formattingRaw, ok := raw["formatting"].(map[string]interface{}); ok {
		if value, ok := toInt(formattingRaw["indentSize"]); ok {
			settings.Formatting.IndentSize = value
		}
		if value, ok := toBool(formattingRaw["alignAmounts"]); ok {
			settings.Formatting.AlignAmounts = value
		}
		if value, ok := toInt(formattingRaw["minAlignmentColumn"]); ok {
			settings.Formatting.MinAlignmentColumn = value
		}
	}
	if value, ok := toInt(raw["formatting.indentSize"]); ok {
		settings.Formatting.IndentSize = value
	}
	if value, ok := toBool(raw["formatting.alignAmounts"]); ok {
		settings.Formatting.AlignAmounts = value
	}
	if value, ok := toInt(raw["formatting.minAlignmentColumn"]); ok {
		settings.Formatting.MinAlignmentColumn = value
	}

	// CLI
	if cliRaw, ok := raw["cli"].(map[string]interface{}); ok {
		if value, ok := toBool(cliRaw["enabled"]); ok {
			settings.CLI.Enabled = value
		}
		if value, ok := toString(cliRaw["path"]); ok {
			settings.CLI.Path = value
		}
		if value, ok := toInt(cliRaw["timeout"]); ok {
			settings.CLI.Timeout = time.Duration(value) * time.Millisecond
		}
	}
	if value, ok := toBool(raw["cli.enabled"]); ok {
		settings.CLI.Enabled = value
	}
	if value, ok := toString(raw["cli.path"]); ok {
		settings.CLI.Path = value
	}
	if value, ok := toInt(raw["cli.timeout"]); ok {
		settings.CLI.Timeout = time.Duration(value) * time.Millisecond
	}

	// Limits
	if limitsRaw, ok := raw["limits"].(map[string]interface{}); ok {
		if value, ok := toInt64(limitsRaw["maxFileSizeBytes"]); ok {
			settings.Limits.MaxFileSizeBytes = value
		}
		if value, ok := toInt64(limitsRaw["maxFileSize"]); ok {
			settings.Limits.MaxFileSizeBytes = value
		}
		if value, ok := toInt(limitsRaw["maxIncludeDepth"]); ok {
			settings.Limits.MaxIncludeDepth = value
		}
	}
	if value, ok := toInt64(raw["limits.maxFileSizeBytes"]); ok {
		settings.Limits.MaxFileSizeBytes = value
	}
	if value, ok := toInt64(raw["limits.maxFileSize"]); ok {
		settings.Limits.MaxFileSizeBytes = value
	}
	if value, ok := toInt(raw["limits.maxIncludeDepth"]); ok {
		settings.Limits.MaxIncludeDepth = value
	}

	return settings
}

func toInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func toInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func toBool(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		v = strings.TrimSpace(strings.ToLower(v))
		switch v {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func toString(value interface{}) (string, bool) {
	if v, ok := value.(string); ok {
		return v, true
	}
	return "", false
}
