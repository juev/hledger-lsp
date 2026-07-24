package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/juev/hledger-lsp/internal/include"
	"github.com/juev/hledger-lsp/internal/workspace"
)

func TestTypeHierarchy_LocalDirectAndSyntheticNodes(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///hierarchy.journal")
	srv.StoreDocument(docURI, "account expenses:food\n\n2024-01-01 lunch\n    expenses:food:restaurants  $10\n    expenses:food:groceries  $5\n    assets:cash\n")

	prepared, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 3, Character: 18},
		},
	})
	require.NoError(t, err)
	require.Len(t, prepared, 1)
	assert.Equal(t, "expenses:food:restaurants", prepared[0].Name)

	parents, err := srv.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: prepared[0]})
	require.NoError(t, err)
	require.Len(t, parents, 1)
	assert.Equal(t, "expenses:food", parents[0].Name)

	children, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: parents[0]})
	require.NoError(t, err)
	require.Len(t, children, 2)
	assert.Equal(t, []string{"expenses:food:groceries", "expenses:food:restaurants"}, []string{children[0].Name, children[1].Name})

	rootData, err := protocol.Marshal(typeHierarchyData{Account: "expenses", Origin: docURI})
	require.NoError(t, err)
	rootParents, err := srv.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny(rootData)}})
	require.NoError(t, err)
	assert.Nil(t, rootParents)
}

func TestTypeHierarchy_PreparesAccountDeclarationWithCompleteRange(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///declaration.journal")
	srv.StoreDocument(docURI, "account expenses:food\n")

	prepared, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
			Position:     protocol.Position{Line: 0, Character: 10},
		},
	})
	require.NoError(t, err)
	require.Len(t, prepared, 1)
	assert.Equal(t, "expenses:food", prepared[0].Name)
	assert.Equal(t, protocol.Range{
		Start: protocol.Position{Line: 0, Character: 8},
		End:   protocol.Position{Line: 0, Character: 21},
	}, prepared[0].Range)
}

func TestTypeHierarchy_RejectsMalformedDataAndNonAccountPosition(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///hierarchy.journal")
	srv.StoreDocument(docURI, "2024-01-01 lunch\n    expenses:food  $10\n")

	prepared, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Position: protocol.Position{Line: 0, Character: 1}},
	})
	require.NoError(t, err)
	assert.Nil(t, prepared)

	children, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny([]byte(`{"account":1}`))}})
	require.NoError(t, err)
	assert.Nil(t, children)
}

func TestTypeHierarchy_UsesUTF16RangesAndNormalizesCRLF(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///utf16.journal")
	srv.StoreDocument(docURI, "2024-01-01 😀\r\n    Расходы:🍜  $10\r\n")

	prepared, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Position: protocol.Position{Line: 1, Character: 12}},
	})
	require.NoError(t, err)
	require.Len(t, prepared, 1)
	assert.Equal(t, protocol.Position{Line: 1, Character: 4}, prepared[0].Range.Start)
	assert.Equal(t, protocol.Position{Line: 1, Character: 14}, prepared[0].Range.End)
	assert.Equal(t, prepared[0].Range, prepared[0].SelectionRange)

	miss, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Position: protocol.Position{Line: 1, Character: 14}},
	})
	require.NoError(t, err)
	assert.Nil(t, miss)
}

func TestTypeHierarchy_CombinesRootAndIncludedAccounts(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")
	main := "include child.journal\n2024-01-01 root\n    expenses:root  $1\n    assets:cash\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(main), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte("2024-01-01 child\n    expenses:child  $1\n    assets:cash\n"), 0o644))

	srv := NewServer()
	mainURI := uri.File(mainPath)
	srv.StoreDocument(mainURI, main)
	resolved, errs := srv.loader.Load(mainPath)
	require.Empty(t, errs)
	srv.resolved.Store(mainURI, resolved)

	data, err := protocol.Marshal(typeHierarchyData{Account: "expenses", Origin: mainURI})
	require.NoError(t, err)
	children, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny(data)}})
	require.NoError(t, err)
	require.Len(t, children, 2)
	assert.Equal(t, []string{"expenses:child", "expenses:root"}, []string{children[0].Name, children[1].Name})
}

func TestTypeHierarchy_PreservesRepeatedIncludeContexts(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")
	main := "apply account first\ninclude child.journal\nend apply account\napply account second\ninclude child.journal\nend apply account\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(main), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte("2024-01-01 child\n    cash  $1\n    assets:y\n"), 0o644))

	srv := NewServer()
	mainURI := uri.File(mainPath)
	srv.StoreDocument(mainURI, main)
	resolved, errs := srv.loader.Load(mainPath)
	require.Empty(t, errs)
	srv.resolved.Store(mainURI, resolved)

	firstData, err := protocol.Marshal(typeHierarchyData{Account: "first", Origin: mainURI})
	require.NoError(t, err)
	first, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny(firstData)}})
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, []string{"first:assets", "first:cash"}, []string{first[0].Name, first[1].Name})

	secondData, err := protocol.Marshal(typeHierarchyData{Account: "second", Origin: mainURI})
	require.NoError(t, err)
	second, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny(secondData)}})
	require.NoError(t, err)
	require.Len(t, second, 2)
	assert.Equal(t, []string{"second:assets", "second:cash"}, []string{second[0].Name, second[1].Name})
}

func TestTypeHierarchy_PrefersExactOccurrenceOverDescendantDeclaration(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("file:///exact-anchor.journal")
	srv.StoreDocument(docURI, "account expenses:food:restaurants\n\n2024-01-01 lunch\n    expenses:food  $10\n    assets:cash\n")

	data, err := protocol.Marshal(typeHierarchyData{Account: "expenses:food", Origin: docURI})
	require.NoError(t, err)
	children, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny(data)}})
	require.NoError(t, err)
	require.Len(t, children, 1)

	parent, err := srv.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: children[0]})
	require.NoError(t, err)
	require.Len(t, parent, 1)
	assert.Equal(t, protocol.Position{Line: 3, Character: 4}, parent[0].Range.Start)
}

func TestTypeHierarchy_PrepareIncludedAccountUsesAllOccurrenceContexts(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.journal")
	childPath := filepath.Join(dir, "child.journal")
	main := "apply account first\ninclude child.journal\nend apply account\napply account second\ninclude child.journal\nend apply account\n"
	child := "2024-01-01 child\n    cash  $1\n    assets:y\n"
	require.NoError(t, os.WriteFile(mainPath, []byte(main), 0o644))
	require.NoError(t, os.WriteFile(childPath, []byte(child), 0o644))

	srv := NewServer()
	childURI := uri.File(childPath)
	srv.StoreDocument(childURI, child)
	resolved, errs := srv.loader.Load(mainPath)
	require.Empty(t, errs)
	srv.resolved.Store(childURI, resolved)

	items, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: childURI}, Position: protocol.Position{Line: 1, Character: 5}},
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{"first:cash", "second:cash"}, []string{items[0].Name, items[1].Name})
	for i, item := range items {
		data, ok := decodeTypeHierarchyData(item.Data)
		require.True(t, ok)
		assert.Equal(t, childURI, data.Origin)
		parents, err := srv.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: item})
		require.NoError(t, err)
		require.Len(t, parents, 1)
		assert.Equal(t, []string{"first", "second"}[i], parents[0].Name)
	}
}

func TestTypeHierarchy_WorkspaceOwnersKeepApplyAccountContext(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.journal")
	firstRootPath := filepath.Join(dir, "a-root.journal")
	secondRootPath := filepath.Join(dir, "b-root.journal")
	child := "2024-01-01 child\n    cash  $1\n    assets:y\n"
	require.NoError(t, os.WriteFile(childPath, []byte(child), 0o644))
	require.NoError(t, os.WriteFile(firstRootPath, []byte("apply account first\ninclude child.journal\nend apply account\n"), 0o644))
	require.NoError(t, os.WriteFile(secondRootPath, []byte("apply account second\ninclude child.journal\nend apply account\n"), 0o644))

	srv := NewServer()
	srv.loader = include.NewLoader()
	srv.workspace = workspace.NewWorkspace(dir, srv.loader)
	require.NoError(t, srv.workspace.Initialize())
	childURI := uri.File(childPath)
	srv.StoreDocument(childURI, child)

	items, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: childURI}, Position: protocol.Position{Line: 1, Character: 5}},
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{"first:cash", "second:cash"}, []string{items[0].Name, items[1].Name})

	for i, item := range items {
		data, ok := decodeTypeHierarchyData(item.Data)
		require.True(t, ok)
		assert.Equal(t, []string{firstRootPath, secondRootPath}[i], data.RootPath)
		parents, err := srv.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: item})
		require.NoError(t, err)
		require.Len(t, parents, 1)
		assert.Equal(t, []string{"first", "second"}[i], parents[0].Name)
	}
}

func TestTypeHierarchy_RejectsUnknownWorkspaceRootPath(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.journal")
	rootPath := filepath.Join(dir, "root.journal")
	child := "2024-01-01 child\n    cash  $1\n    assets:y\n"
	require.NoError(t, os.WriteFile(childPath, []byte(child), 0o644))
	require.NoError(t, os.WriteFile(rootPath, []byte("apply account first\ninclude child.journal\nend apply account\n"), 0o644))

	srv := NewServer()
	srv.loader = include.NewLoader()
	srv.workspace = workspace.NewWorkspace(dir, srv.loader)
	require.NoError(t, srv.workspace.Initialize())
	childURI := uri.File(childPath)
	srv.StoreDocument(childURI, child)

	data, err := protocol.Marshal(typeHierarchyData{Account: "assets", Origin: childURI, RootPath: filepath.Join(dir, "missing.journal")})
	require.NoError(t, err)
	children, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: protocol.TypeHierarchyItem{Data: protocol.LSPAny(data)}})
	require.NoError(t, err)
	assert.Nil(t, children)
}

func TestTypeHierarchy_UntitledDocumentKeepsURIForFollowups(t *testing.T) {
	srv := NewServer()
	docURI := uri.URI("untitled:hledger")
	srv.StoreDocument(docURI, "2024-01-01 lunch\n    expenses:food  $10\n    assets:cash\n")

	items, err := srv.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: docURI}, Position: protocol.Position{Line: 1, Character: 12}},
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, docURI, items[0].URI)

	parents, err := srv.Supertypes(context.Background(), &protocol.TypeHierarchySupertypesParams{Item: items[0]})
	require.NoError(t, err)
	require.Len(t, parents, 1)
	assert.Equal(t, "expenses", parents[0].Name)
	assert.Equal(t, docURI, parents[0].URI)

	children, err := srv.Subtypes(context.Background(), &protocol.TypeHierarchySubtypesParams{Item: parents[0]})
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, "expenses:food", children[0].Name)
	assert.Equal(t, docURI, children[0].URI)
}
