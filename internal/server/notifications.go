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

// warnOnce logs a warning for a document at most once per content revision, so a
// recurring condition (a file above the size limit, an unresolvable include)
// does not flood the client log on every keystroke.
func (s *Server) warnOnce(docURI uri.URI, message string) {
	if message == "" {
		return
	}

	key := string(docURI) + "\x00" + message
	if _, loaded := s.warned.LoadOrStore(key, true); loaded {
		return
	}
	s.logMessage(protocol.MessageTypeWarning, message)
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
