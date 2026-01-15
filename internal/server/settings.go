package server

import (
	"context"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/include"
)

type completionSettings struct {
	MaxResults int
}

type serverSettings struct {
	Completion completionSettings
	Limits     include.Limits
}

func defaultServerSettings() serverSettings {
	return serverSettings{
		Completion: completionSettings{
			MaxResults: 50,
		},
		Limits: include.DefaultLimits(),
	}
}

func normalizeServerSettings(settings serverSettings) serverSettings {
	defaults := defaultServerSettings()
	if settings.Completion.MaxResults <= 0 {
		settings.Completion.MaxResults = defaults.Completion.MaxResults
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
	s.settings = settings
	s.settingsMu.Unlock()
	if s.loader != nil {
		s.loader.SetLimits(settings.Limits)
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
	if completionRaw, ok := raw["completion"].(map[string]interface{}); ok {
		if value, ok := toInt(completionRaw["maxResults"]); ok {
			settings.Completion.MaxResults = value
		}
	}
	if value, ok := toInt(raw["completion.maxResults"]); ok {
		settings.Completion.MaxResults = value
	}

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
