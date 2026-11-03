package server

import (
	"context"
	"time"

	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/csvimport"
	"github.com/juev/hledger-lsp/internal/filetype"
)

// A journal includes CSV, SSV, TSV and rules files only under the hledger 2
// target. The configured hledger binary converts them; the server never
// interprets CSV rules itself. Under the default target 1 such an include stays
// refused as not a journal, as hledger 1.52 refuses it.

// configureImports gives the loader an importer for the target and CLI
// settings, or none under target 1. "auto" also imports: the importer asks the
// binary for its version and reports an explicit error when hledger is
// disabled, missing or too old to include CSV files, which is what hledger 1
// would report too.
func (s *Server) configureImports(settings serverSettings) {
	if s.loader == nil {
		return
	}
	if settings.TargetVersion == targetVersion1 {
		s.loader.SetImporter(nil, nil)
		return
	}
	s.loader.SetImporter(csvimport.New(csvimport.Config{
		Enabled: settings.CLI.Enabled,
		Path:    settings.CLI.Path,
		Timeout: settings.CLI.Timeout,
	}), s.importBuffer)
}

// importSettingsChanged reports whether includes of CSV data must be read again.
func importSettingsChanged(old, updated serverSettings) bool {
	if old.TargetVersion != updated.TargetVersion {
		return true
	}
	return updated.TargetVersion != targetVersion1 && old.CLI != updated.CLI
}

// importBuffer returns the editor buffer of an import source, so the importer
// can report one that differs from the saved file it reads.
func (s *Server) importBuffer(path string) (string, bool) {
	return s.GetDocument(pathToURI(path))
}

// isImportSource reports whether the file at path is separated data or CSV
// rules: by its extension, or because an import reads it, under this name or
// another one for the same file. Such a document gets no journal analysis,
// formatting, completion, hints or edits, whether its import succeeded,
// produced nothing or failed.
func (s *Server) isImportSource(path string) bool {
	if path == "" {
		return false
	}
	if csvimport.IsDataPath(path) || filetype.IsRules(path) {
		return true
	}
	return s.loader != nil && s.loader.IsImportSource(path)
}

func (s *Server) isImportDocument(docURI uri.URI) bool {
	return s.isImportSource(uriToPath(docURI))
}

// importSourceChanged reacts to a new state of a file an import reads: its
// saved content or its editor buffer. Every tree that imports is rebuilt, and
// its files republished, once the burst of changes settles.
func (s *Server) importSourceChanged() {
	if s.workspace != nil {
		s.workspace.ReloadImports()
	}
	s.diagMu.Lock()
	delay := s.diagDebounce
	if s.importTimer != nil {
		s.importTimer.Stop()
	}
	s.importTimer = time.AfterFunc(delay, func() { s.republishImports(context.Background()) })
	s.diagMu.Unlock()
}

// republishImports republishes the trees that import CSV data, or every open
// document when there is no workspace to say which those are.
func (s *Server) republishImports(ctx context.Context) {
	if s.workspace == nil {
		s.republishDiagnostics()
		return
	}
	affected := make(map[uri.URI]bool)
	for _, root := range s.workspace.ImportRoots() {
		affected[pathToURI(root)] = true
	}
	s.republishFiles(ctx, affected)
}

// reloadForImportSettings rebuilds everything after a change of target or CLI
// settings, which decides whether and how CSV includes are read.
func (s *Server) reloadForImportSettings(settings serverSettings) {
	s.configureImports(settings)
	if s.workspace != nil {
		s.workspace.ReloadAll()
	}
	s.clearParseCache()
	s.republishDiagnostics()
}
