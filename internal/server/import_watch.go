package server

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// watchedExtensions are the files the server always asks the client to watch.
var watchedExtensions = []string{".journal", ".hledger", ".j", ".ledger", ".prices", ".rules", ".csv", ".ssv", ".tsv"}

// importWatcherID names the registration that watches import sources the
// static patterns miss.
const importWatcherID = "hledger/importSources"

// coveredByStaticWatchers reports whether one of the static patterns already
// watches the file.
func coveredByStaticWatchers(path string) bool {
	for _, extension := range watchedExtensions {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return false
}

// scheduleImportWatchers asks for the watchers of import sources to be brought
// up to date, without waiting: an include with a reader prefix can name a file
// of any extension (csv:data.txt), which no static pattern watches, so without
// its own watcher a change on disk would go unseen until the journal reloads.
func (s *Server) scheduleImportWatchers() {
	if s.client == nil || s.workspace == nil || !s.clientCapabilities.supportsDynamicFileWatchers {
		return
	}
	select {
	case s.importWatchKick <- struct{}{}:
		go s.syncImportWatchers(context.Background())
	default:
		// A pass is already pending and will read the newest sources.
	}
}

// syncImportWatchers registers a watcher for every import source the static
// patterns miss and drops the previous registration when the set changed.
// Passes run one at a time: a pass takes the kick token only once it holds the
// loop, so a kick that arrives during a pass starts the next one.
func (s *Server) syncImportWatchers(ctx context.Context) {
	s.importWatchMu.Lock()
	defer s.importWatchMu.Unlock()
	select {
	case <-s.importWatchKick:
	default:
		return
	}

	var paths []string
	for _, path := range s.workspace.ImportSourcePaths() {
		if !coveredByStaticWatchers(path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if slices.Equal(paths, s.importWatched) {
		return
	}

	if len(s.importWatched) > 0 {
		_ = s.client.UnregisterCapability(ctx, &protocol.UnregistrationParams{
			Unregisterations: []protocol.Unregistration{{ID: importWatcherID, Method: "workspace/didChangeWatchedFiles"}},
		})
	}
	s.importWatched = paths
	if len(paths) == 0 {
		return
	}

	watchers := make([]protocol.FileSystemWatcher, 0, len(paths))
	for _, path := range paths {
		watchers = append(watchers, protocol.FileSystemWatcher{GlobPattern: s.importWatchPattern(path)})
	}
	options, err := protocol.Marshal(protocol.DidChangeWatchedFilesRegistrationOptions{Watchers: watchers})
	if err != nil {
		return
	}
	_ = s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{
			ID:              importWatcherID,
			Method:          "workspace/didChangeWatchedFiles",
			RegisterOptions: protocol.LSPAny(options),
		}},
	})
}

// importWatchPattern watches exactly one file: relative to its directory where
// the client supports that, otherwise by name anywhere in the workspace, which
// may also report a same-named file elsewhere and only costs a reload.
func (s *Server) importWatchPattern(path string) protocol.GlobPattern {
	name := escapeGlob(filepath.Base(path))
	if s.clientCapabilities.supportsRelativeWatchPatterns {
		return &protocol.RelativePattern{
			BaseURI: protocol.URI(uri.File(filepath.Dir(path))),
			Pattern: protocol.Pattern(name),
		}
	}
	return protocol.Pattern("**/" + name)
}

// escapeGlob makes every character of a file name match itself in an LSP glob.
func escapeGlob(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '*', '?', '[', ']', '{', '}':
			b.WriteByte('[')
			b.WriteRune(r)
			b.WriteByte(']')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
