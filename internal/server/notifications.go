package server

import (
	"context"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// logMessage reports something the user may want to inspect, without stealing
// focus. The server previously stayed silent about everything: skipped files,
// include failures and CLI errors were invisible.
func (s *Server) logMessage(severity protocol.MessageType, message string) {
	if s.client == nil || message == "" {
		return
	}
	_ = s.client.LogMessage(context.Background(), &protocol.LogMessageParams{
		Type:    severity,
		Message: message,
	})
}

// warnOnce logs a warning for a document at most once per condition. The key must
// be stable across edits (an error code and a path, not a message that embeds the
// file size), so a user who keeps typing does not get the same warning per
// keystroke and the bookkeeping stays bounded.
func (s *Server) warnOnce(docURI uri.URI, key, message string) {
	if message == "" || key == "" {
		return
	}

	loaded, ok := s.warned.Load(docURI)
	var conditions map[string]bool
	if ok {
		conditions, _ = loaded.(map[string]bool)
	}
	if conditions == nil {
		conditions = make(map[string]bool)
	}

	s.warnedMu.Lock()
	if conditions[key] {
		s.warnedMu.Unlock()
		return
	}
	conditions[key] = true
	s.warned.Store(docURI, conditions)
	s.warnedMu.Unlock()

	s.logMessage(protocol.MessageTypeWarning, message)
}

// forgetWarnings drops the recorded warnings for a closed document so the
// bookkeeping does not grow for the lifetime of the session.
func (s *Server) forgetWarnings(docURI uri.URI) {
	s.warned.Delete(docURI)
}

// showMessage asks the client to surface a message in the user interface. It is
// reserved for conditions that need attention.
func (s *Server) showMessage(severity protocol.MessageType, message string) {
	if s.client == nil || message == "" {
		return
	}
	_ = s.client.ShowMessage(context.Background(), &protocol.ShowMessageParams{
		Type:    severity,
		Message: message,
	})
}
