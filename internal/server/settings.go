package server

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/analyzer"
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
	CodeLens         bool
	InlayHints       bool
}

type inlayHintSettings struct {
	InferredAmounts bool
	RunningBalances bool
	CostExpansion   bool
}

// Account scope values for completion.accountScope.
const (
	accountScopeNonzero = "nonzero"
	accountScopeAll     = "all"
)

type completionSettings struct {
	MaxResults    int
	FuzzyMatching bool
	ShowCounts    bool
	IncludeNotes  bool
	// AccountScope selects whether account completion hides accounts whose
	// balance is zero in every commodity. hledger users often keep closed
	// accounts around, so "all" is available without the experimental
	// request-local scope.
	AccountScope string
}

// Account declaration check modes. hledger only requires every account to be
// declared under --strict, so the default is off; "lint" keeps the historical
// soft check, "strict" mirrors --strict.
const (
	accountCheckOff    = "off"
	accountCheckLint   = "lint"
	accountCheckStrict = "strict"
)

type diagnosticsSettings struct {
	AccountCheck           string
	UndeclaredCommodities  bool
	UnbalancedTransactions bool
	BalanceAssertions      bool
	BalanceTolerance       float64
	DebounceMs             int
}

type formattingSettings struct {
	IndentSize            int
	AlignAmounts          bool
	MinAlignmentColumn    int
	AmountAlignmentColumn int
	AmountAlignmentMode   string
	AmountAlignmentTarget string
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
	InlayHints  inlayHintSettings
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
			InlineCompletion: true,
			CodeLens:         false,
			InlayHints:       true,
		},
		InlayHints: inlayHintSettings{InferredAmounts: true},
		Completion: completionSettings{
			MaxResults:    50,
			FuzzyMatching: true,
			ShowCounts:    true,
			IncludeNotes:  true,
			AccountScope:  accountScopeNonzero,
		},
		Diagnostics: diagnosticsSettings{
			AccountCheck:           accountCheckOff,
			UndeclaredCommodities:  false,
			UnbalancedTransactions: true,
			BalanceAssertions:      true,
			DebounceMs:             100,
		},
		Formatting: formattingSettings{
			IndentSize:          4,
			AlignAmounts:        true,
			MinAlignmentColumn:  0,
			AmountAlignmentMode: "right",
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
	switch settings.Formatting.AmountAlignmentMode {
	case "right", "decimal", "left":
		// valid
	default:
		settings.Formatting.AmountAlignmentMode = defaults.Formatting.AmountAlignmentMode
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
	switch settings.Completion.AccountScope {
	case accountScopeNonzero, accountScopeAll:
		// valid
	default:
		settings.Completion.AccountScope = defaults.Completion.AccountScope
	}
	switch settings.Diagnostics.AccountCheck {
	case accountCheckOff, accountCheckLint, accountCheckStrict:
		// valid
	default:
		settings.Diagnostics.AccountCheck = defaults.Diagnostics.AccountCheck
	}
	if settings.Diagnostics.DebounceMs <= 0 {
		settings.Diagnostics.DebounceMs = defaults.Diagnostics.DebounceMs
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
	if s.analyzer != nil {
		s.analyzer.BalanceTolerance = decimal.NewFromFloat(settings.Diagnostics.BalanceTolerance)
		s.analyzer.AccountCheck = accountCheckMode(settings.Diagnostics.AccountCheck)
	}
	// Only a real settings change updates the debounce, so tests (and callers)
	// that set the debounce directly keep their value.
	if oldSettings.Diagnostics.DebounceMs != settings.Diagnostics.DebounceMs {
		s.diagDebounce = time.Duration(settings.Diagnostics.DebounceMs) * time.Millisecond
	}
	if oldSettings.Diagnostics.BalanceTolerance != settings.Diagnostics.BalanceTolerance ||
		oldSettings.Diagnostics.AccountCheck != settings.Diagnostics.AccountCheck {
		s.invalidateJournalDiagnostics()
	}
	if diagnosticsSettingsChanged(oldSettings.Diagnostics, settings.Diagnostics) {
		s.republishDiagnostics()
	}
	if oldSettings.Formatting.IndentSize != settings.Formatting.IndentSize ||
		oldSettings.Formatting.MinAlignmentColumn != settings.Formatting.MinAlignmentColumn ||
		oldSettings.Formatting.AmountAlignmentColumn != settings.Formatting.AmountAlignmentColumn ||
		oldSettings.Formatting.AmountAlignmentMode != settings.Formatting.AmountAlignmentMode {
		s.clearAlignmentCache()
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
			{Section: stringPtr("hledger")},
		},
	})
	if err != nil || len(result) == 0 {
		return
	}
	settings := parseSettingsFromLSPAny(s.getSettings(), result[0])
	s.setSettings(settings)
}

// DidChangeConfiguration refreshes cached settings but cannot change
// registered capabilities — feature flag changes require a server restart.
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

func parseSettingsFromLSPAny(base serverSettings, raw protocol.LSPAny) serverSettings {
	if len(raw) == 0 {
		return normalizeServerSettings(base)
	}
	var decoded map[string]interface{}
	if err := protocol.Unmarshal(raw, &decoded); err != nil {
		return normalizeServerSettings(base)
	}
	return parseSettingsFromRaw(base, decoded)
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
		if value, ok := toBool(featuresRaw["codeLens"]); ok {
			settings.Features.CodeLens = value
		}
		if value, ok := toBool(featuresRaw["inlayHints"]); ok {
			settings.Features.InlayHints = value
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
	if value, ok := toBool(raw["features.codeLens"]); ok {
		settings.Features.CodeLens = value
	}
	if value, ok := toBool(raw["features.inlayHints"]); ok {
		settings.Features.InlayHints = value
	}

	// Inlay hints
	if inlayHintsRaw, ok := raw["inlayHints"].(map[string]interface{}); ok {
		if value, ok := toBool(inlayHintsRaw["inferredAmounts"]); ok {
			settings.InlayHints.InferredAmounts = value
		}
		if value, ok := toBool(inlayHintsRaw["runningBalances"]); ok {
			settings.InlayHints.RunningBalances = value
		}
		if value, ok := toBool(inlayHintsRaw["costExpansion"]); ok {
			settings.InlayHints.CostExpansion = value
		}
	}
	if value, ok := toBool(raw["inlayHints.inferredAmounts"]); ok {
		settings.InlayHints.InferredAmounts = value
	}
	if value, ok := toBool(raw["inlayHints.runningBalances"]); ok {
		settings.InlayHints.RunningBalances = value
	}
	if value, ok := toBool(raw["inlayHints.costExpansion"]); ok {
		settings.InlayHints.CostExpansion = value
	}

	// Completion
	if completionRaw, ok := raw["completion"].(map[string]interface{}); ok {
		if value, ok := toInt(completionRaw["maxResults"]); ok {
			settings.Completion.MaxResults = value
		}
		if value, ok := toBool(completionRaw["fuzzyMatching"]); ok {
			settings.Completion.FuzzyMatching = value
		}
		if value, ok := toBool(completionRaw["showCounts"]); ok {
			settings.Completion.ShowCounts = value
		}
		if value, ok := toBool(completionRaw["includeNotes"]); ok {
			settings.Completion.IncludeNotes = value
		}
		if value, ok := toString(completionRaw["accountScope"]); ok {
			settings.Completion.AccountScope = value
		}
	}
	if value, ok := toInt(raw["completion.maxResults"]); ok {
		settings.Completion.MaxResults = value
	}
	if value, ok := toBool(raw["completion.fuzzyMatching"]); ok {
		settings.Completion.FuzzyMatching = value
	}
	if value, ok := toBool(raw["completion.showCounts"]); ok {
		settings.Completion.ShowCounts = value
	}
	if value, ok := toBool(raw["completion.includeNotes"]); ok {
		settings.Completion.IncludeNotes = value
	}
	if value, ok := toString(raw["completion.accountScope"]); ok {
		settings.Completion.AccountScope = value
	}

	// Diagnostics. The legacy boolean undeclaredAccounts is still honoured: it
	// maps to the "lint" mode unless accountCheck is set explicitly.
	accountCheckSet := false
	legacyUndeclared := false
	hasLegacyUndeclared := false
	if diagnosticsRaw, ok := raw["diagnostics"].(map[string]interface{}); ok {
		if value, ok := toString(diagnosticsRaw["accountCheck"]); ok {
			settings.Diagnostics.AccountCheck = value
			accountCheckSet = true
		}
		if value, ok := toBool(diagnosticsRaw["undeclaredAccounts"]); ok {
			legacyUndeclared, hasLegacyUndeclared = value, true
		}
		if value, ok := toBool(diagnosticsRaw["undeclaredCommodities"]); ok {
			settings.Diagnostics.UndeclaredCommodities = value
		}
		if value, ok := toBool(diagnosticsRaw["unbalancedTransactions"]); ok {
			settings.Diagnostics.UnbalancedTransactions = value
		}
		if value, ok := toBool(diagnosticsRaw["balanceAssertions"]); ok {
			settings.Diagnostics.BalanceAssertions = value
		}
		if value, ok := toFloat64(diagnosticsRaw["balanceTolerance"]); ok {
			settings.Diagnostics.BalanceTolerance = value
		}
		if value, ok := toInt(diagnosticsRaw["debounceMs"]); ok {
			settings.Diagnostics.DebounceMs = value
		}
	}
	if value, ok := toString(raw["diagnostics.accountCheck"]); ok {
		settings.Diagnostics.AccountCheck = value
		accountCheckSet = true
	}
	if value, ok := toBool(raw["diagnostics.undeclaredAccounts"]); ok {
		legacyUndeclared, hasLegacyUndeclared = value, true
	}
	if value, ok := toBool(raw["diagnostics.balanceAssertions"]); ok {
		settings.Diagnostics.BalanceAssertions = value
	}
	if value, ok := toInt(raw["diagnostics.debounceMs"]); ok {
		settings.Diagnostics.DebounceMs = value
	}
	if !accountCheckSet && hasLegacyUndeclared && legacyUndeclared {
		settings.Diagnostics.AccountCheck = accountCheckLint
	}
	if value, ok := toBool(raw["diagnostics.undeclaredCommodities"]); ok {
		settings.Diagnostics.UndeclaredCommodities = value
	}
	if value, ok := toBool(raw["diagnostics.unbalancedTransactions"]); ok {
		settings.Diagnostics.UnbalancedTransactions = value
	}
	if value, ok := toFloat64(raw["diagnostics.balanceTolerance"]); ok {
		settings.Diagnostics.BalanceTolerance = value
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
		if value, ok := toInt(formattingRaw["amountAlignmentColumn"]); ok {
			settings.Formatting.AmountAlignmentColumn = value
		}
		if value, ok := toString(formattingRaw["amountAlignmentMode"]); ok {
			settings.Formatting.AmountAlignmentMode = value
		}
		if value, ok := toString(formattingRaw["amountAlignmentTarget"]); ok {
			settings.Formatting.AmountAlignmentTarget = value
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
	if value, ok := toInt(raw["formatting.amountAlignmentColumn"]); ok {
		settings.Formatting.AmountAlignmentColumn = value
	}
	if value, ok := toString(raw["formatting.amountAlignmentMode"]); ok {
		settings.Formatting.AmountAlignmentMode = value
	}
	if value, ok := toString(raw["formatting.amountAlignmentTarget"]); ok {
		settings.Formatting.AmountAlignmentTarget = value
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

func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func toString(value interface{}) (string, bool) {
	if v, ok := value.(string); ok {
		return v, true
	}
	return "", false
}

// accountCheckMode maps the settings string to the analyzer's check mode.
func accountCheckMode(value string) analyzer.AccountCheckMode {
	switch value {
	case accountCheckStrict:
		return analyzer.AccountCheckStrict
	case accountCheckLint:
		return analyzer.AccountCheckLint
	default:
		return analyzer.AccountCheckOff
	}
}

// diagnosticsSettingsChanged reports whether a change affects published
// diagnostics, so cached results must be dropped and open documents republished.
func diagnosticsSettingsChanged(old, updated diagnosticsSettings) bool {
	return old != updated
}
