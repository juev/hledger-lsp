package server

import (
	"os"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/ast"
	"github.com/juev/hledger-lsp/internal/lsputil"
	"github.com/juev/hledger-lsp/internal/textutil"
)

// astRangeToLSP converts a parser range to an LSP range through byte offsets.
//
// Prefer it over astRangeToProtocol, which reports lexer columns as LSP
// characters: the lexer counts columns in runes, while LSP counts UTF-16 code
// units, so every range on a line containing an emoji comes out too short.
// Offsets carry no such ambiguity.
func astRangeToLSP(mapper *lsputil.PositionMapper, rng ast.Range) protocol.Range {
	return protocol.Range{
		Start: mapper.ByteToLSP(rng.Start.Offset),
		End:   mapper.ByteToLSP(rng.End.Offset),
	}
}

// documentSource returns the content of a journal, preferring unsaved editor
// state over what is on disk. The result is normalized, so offsets taken from
// a journal parsed out of it line up with a PositionMapper built on it.
func (s *Server) documentSource(docURI uri.URI) (string, bool) {
	if content, ok := s.GetDocument(docURI); ok {
		return textutil.NormalizeLineEndings(content), true
	}
	content, err := os.ReadFile(uriToPath(docURI))
	if err != nil {
		return "", false
	}
	return textutil.NormalizeLineEndings(string(content)), true
}

// mapperFor builds a position mapper for a journal the server may not have
// open. Returns nil when the file cannot be read.
func (s *Server) mapperFor(docURI uri.URI) *lsputil.PositionMapper {
	content, ok := s.documentSource(docURI)
	if !ok {
		return nil
	}
	return lsputil.NewPositionMapper(content)
}

// mapperCache hands out one position mapper per journal path for requests that
// answer across an include tree, so a file is read and indexed at most once.
type mapperCache struct {
	load    func(path string) *lsputil.PositionMapper
	mappers map[string]*lsputil.PositionMapper
}

func (s *Server) newMapperCache() *mapperCache {
	return &mapperCache{
		load:    func(path string) *lsputil.PositionMapper { return s.mapperFor(pathToURI(path)) },
		mappers: make(map[string]*lsputil.PositionMapper),
	}
}

// rangeIn converts an AST range that belongs to the journal at path. It falls
// back to the column-based conversion when the file cannot be read, which is
// off by one per surrogate pair but better than dropping the location.
func (c *mapperCache) rangeIn(path string, rng ast.Range) protocol.Range {
	if c == nil {
		return *astRangeToProtocol(rng)
	}
	mapper, ok := c.mappers[path]
	if !ok {
		mapper = c.load(path)
		c.mappers[path] = mapper
	}
	if mapper == nil {
		return *astRangeToProtocol(rng)
	}
	return astRangeToLSP(mapper, rng)
}

// rangePtr is a small convenience for fields that hold an optional range.
func rangePtr(rng protocol.Range) *protocol.Range { return &rng }
