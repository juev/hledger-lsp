package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"
)

func TestSemanticTokens_Legend(t *testing.T) {
	legend := GetSemanticTokensLegend()

	require.Len(t, legend.TokenTypes, 12)
	assert.Equal(t, protocol.SemanticTokenNamespace, legend.TokenTypes[0])
	assert.Equal(t, protocol.SemanticTokenType, legend.TokenTypes[1])
	assert.Equal(t, protocol.SemanticTokenFunction, legend.TokenTypes[2])
	assert.Equal(t, protocol.SemanticTokenNumber, legend.TokenTypes[3])
	assert.Equal(t, protocol.SemanticTokenNumber, legend.TokenTypes[4])
	assert.Equal(t, protocol.SemanticTokenTypes("decorator"), legend.TokenTypes[5])
	assert.Equal(t, protocol.SemanticTokenKeyword, legend.TokenTypes[6])
	assert.Equal(t, protocol.SemanticTokenString, legend.TokenTypes[7])
	assert.Equal(t, protocol.SemanticTokenOperator, legend.TokenTypes[8])
	assert.Equal(t, protocol.SemanticTokenComment, legend.TokenTypes[9])
	assert.Equal(t, protocol.SemanticTokenRegexp, legend.TokenTypes[10])
	assert.Equal(t, protocol.SemanticTokenParameter, legend.TokenTypes[11])

	assert.Contains(t, legend.TokenModifiers, protocol.SemanticTokenModifierAbstract)
}

func TestSemanticTokens_Encode(t *testing.T) {
	encoder := NewSemanticTokenEncoder()

	data := encoder.Encode(0, 0, 10, 0, 0)
	assert.Equal(t, []uint32{0, 0, 10, 0, 0}, data)

	data = encoder.Encode(0, 11, 1, 1, 0)
	assert.Equal(t, []uint32{0, 11, 1, 1, 0}, data)

	data = encoder.Encode(1, 4, 13, 1, 0)
	assert.Equal(t, []uint32{1, 4, 13, 1, 0}, data)
}

func TestSemanticTokens_SimpleTransaction(t *testing.T) {
	srv := NewServer()
	content := `2024-01-15 test
    expenses:food  $50
    assets:cash`

	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), content)

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Data)
	assert.Equal(t, 0, len(result.Data)%5)
}

func TestSemanticTokens_EmptyDocument(t *testing.T) {
	srv := NewServer()
	srv.documents.Store(protocol.DocumentURI("file:///test.journal"), "")

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///test.journal",
		},
	}

	result, err := srv.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Data)
}

func TestSemanticTokens_DocumentNotFound(t *testing.T) {
	srv := NewServer()

	params := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: "file:///nonexistent.journal",
		},
	}

	result, err := srv.SemanticTokensFull(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Data)
}

func TestSemanticTokens_CapabilityRegistration(t *testing.T) {
	srv := NewServer()
	params := &protocol.InitializeParams{}

	result, err := srv.Initialize(context.Background(), params)
	require.NoError(t, err)

	opts, ok := result.Capabilities.SemanticTokensProvider.(*SemanticTokensServerCapabilities)
	require.True(t, ok, "SemanticTokensProvider should be *SemanticTokensServerCapabilities")

	assert.NotEmpty(t, opts.Legend.TokenTypes)
	assert.NotEmpty(t, opts.Legend.TokenModifiers)
	assert.NotNil(t, opts.Full)
	assert.True(t, opts.Range)
}

func TestSemanticTokens_TokenTypes(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantType uint32
	}{
		{
			name:     "directive uses directive type",
			content:  "account expenses:food",
			wantType: TokenTypeDirective,
		},
		{
			name:     "date uses date type",
			content:  "2024-01-15 test",
			wantType: TokenTypeDate,
		},
		{
			name:     "payee uses payee type",
			content:  "2024-01-15 grocery store",
			wantType: TokenTypePayee,
		},
		{
			name:     "code uses code type",
			content:  "2024-01-15 (123) test",
			wantType: TokenTypeCode,
		},
		{
			name:     "account uses account type",
			content:  "2024-01-15 test\n    expenses:food  $50",
			wantType: TokenTypeAccount,
		},
		{
			name:     "amount uses amount type",
			content:  "2024-01-15 test\n    expenses:food  50",
			wantType: TokenTypeAmount,
		},
		{
			name:     "commodity uses commodity type",
			content:  "2024-01-15 test\n    expenses:food  $50",
			wantType: TokenTypeCommodity,
		},
		{
			name:     "status uses status type",
			content:  "2024-01-15 * test",
			wantType: TokenTypeStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			found := false
			for _, tok := range tokens {
				if tok.tokenType == tt.wantType {
					found = true
					break
				}
			}
			assert.True(t, found, "expected token type %d not found in tokens", tt.wantType)
		})
	}
}

func TestSemanticTokens_DeclarationModifier(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "account directive marks account as declaration",
			content: "account expenses:food",
		},
		{
			name:    "commodity directive marks commodity as declaration",
			content: "commodity USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			foundDeclaration := false
			for _, tok := range tokens {
				if tok.modifiers&(1<<ModifierDeclaration) != 0 {
					foundDeclaration = true
					break
				}
			}
			assert.True(t, foundDeclaration, "expected declaration modifier not found")
		})
	}
}

func TestSemanticTokens_Range(t *testing.T) {
	srv := NewServer()
	content := `2024-01-01 tx1
    expenses:food  $10
2024-01-02 tx2
    expenses:rent  $100
2024-01-03 tx3
    assets:cash  $50`

	uri := protocol.DocumentURI("file:///test.journal")
	srv.documents.Store(uri, content)

	params := &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 2, Character: 0},
			End:   protocol.Position{Line: 3, Character: 100},
		},
	}

	result, err := srv.SemanticTokensRange(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Data)

	fullParams := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	fullResult, err := srv.SemanticTokensFull(context.Background(), fullParams)
	require.NoError(t, err)

	assert.Less(t, len(result.Data), len(fullResult.Data),
		"range result should have fewer tokens than full result")
}

func TestSemanticTokens_Delta(t *testing.T) {
	srv := NewServer()
	uri := protocol.DocumentURI("file:///test.journal")

	content1 := `2024-01-01 test
    expenses:food  $10`
	srv.documents.Store(uri, content1)

	fullParams := &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	fullResult, err := srv.SemanticTokensFull(context.Background(), fullParams)
	require.NoError(t, err)
	require.NotEmpty(t, fullResult.ResultID)

	content2 := `2024-01-01 test
    expenses:food  $20`
	srv.documents.Store(uri, content2)

	deltaParams := &protocol.SemanticTokensDeltaParams{
		TextDocument:     protocol.TextDocumentIdentifier{URI: uri},
		PreviousResultID: fullResult.ResultID,
	}

	deltaResult, err := srv.SemanticTokensFullDelta(context.Background(), deltaParams)
	require.NoError(t, err)
	require.NotNil(t, deltaResult)

	switch result := deltaResult.(type) {
	case *protocol.SemanticTokens:
		assert.NotEmpty(t, result.Data)
	case *protocol.SemanticTokensDelta:
		assert.NotNil(t, result.Edits)
	default:
		t.Fatalf("unexpected result type: %T", deltaResult)
	}
}

func TestSemanticTokens_CommentLength(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantLength uint32
	}{
		{
			name:       "single semicolon comment",
			content:    "; test",
			wantLength: 6,
		},
		{
			name:       "double semicolon comment",
			content:    ";; test",
			wantLength: 7,
		},
		{
			name:       "triple semicolon comment",
			content:    ";;; test",
			wantLength: 8,
		},
		{
			name:       "double semicolon with date",
			content:    ";;  01-12",
			wantLength: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			var commentToken *semanticToken
			for i := range tokens {
				if tokens[i].tokenType == TokenTypeComment {
					commentToken = &tokens[i]
					break
				}
			}
			require.NotNil(t, commentToken, "comment token not found")
			assert.Equal(t, tt.wantLength, commentToken.length,
				"comment length mismatch for %q", tt.content)
		})
	}
}

func TestSemanticTokens_TagsInComments(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantTags   int
		wantTagPos []struct {
			col    uint32
			length uint32
		}
	}{
		{
			name:     "single tag with value",
			content:  "; client:acme",
			wantTags: 1,
			wantTagPos: []struct {
				col    uint32
				length uint32
			}{
				{col: 2, length: 7}, // "client:" = 6+1 = 7 (value is now separate token)
			},
		},
		{
			name:     "multiple tags",
			content:  "; client:acme, project:alpha",
			wantTags: 2,
			wantTagPos: []struct {
				col    uint32
				length uint32
			}{
				{col: 2, length: 7},  // "client:" = 6+1 = 7
				{col: 15, length: 8}, // "project:" = 7+1 = 8
			},
		},
		{
			name:     "tag without value",
			content:  "; billable:",
			wantTags: 1,
			wantTagPos: []struct {
				col    uint32
				length uint32
			}{
				{col: 2, length: 9}, // "billable:" = 8+1 = 9
			},
		},
		{
			name:     "tag in transaction comment",
			content:  "2024-01-15 test  ; date:2024-01-20",
			wantTags: 1,
		},
		{
			name:     "unicode tag name (cyrillic)",
			content:  "; клиент:acme",
			wantTags: 1,
		},
		{
			name:     "unicode tag name (chinese)",
			content:  "; 项目:测试",
			wantTags: 1,
		},
		{
			name:     "date tag with space after colon",
			content:  "; date: 2024-01-20",
			wantTags: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens, "expected tokens")

			var tagTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag {
					tagTokens = append(tagTokens, tok)
				}
			}

			assert.Len(t, tagTokens, tt.wantTags, "expected %d tag tokens, got %d", tt.wantTags, len(tagTokens))

			if tt.wantTagPos != nil {
				for i, pos := range tt.wantTagPos {
					if i < len(tagTokens) {
						assert.Equal(t, pos.col, tagTokens[i].col, "tag %d column mismatch", i)
						assert.Equal(t, pos.length, tagTokens[i].length, "tag %d length mismatch", i)
					}
				}
			}
		})
	}
}

func TestSemanticTokens_TagNameAndValueSeparate(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantTagTokens   int
		wantValueTokens int
		wantPositions   []struct {
			tokenType uint32
			col       uint32
			length    uint32
		}
	}{
		{
			name:            "tag with value split into two tokens",
			content:         "; client:acme",
			wantTagTokens:   1,
			wantValueTokens: 1,
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 7},      // "client:" = 7
				{tokenType: TokenTypeTagValue, col: 9, length: 4}, // "acme" = 4
			},
		},
		{
			name:            "tag without value has no value token",
			content:         "; billable:",
			wantTagTokens:   1,
			wantValueTokens: 0,
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 9}, // "billable:" = 9
			},
		},
		{
			name:            "multiple tags with values",
			content:         "; client:acme, project:alpha",
			wantTagTokens:   2,
			wantValueTokens: 2,
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 7},       // "client:" = 7
				{tokenType: TokenTypeTagValue, col: 9, length: 4},  // "acme" = 4
				{tokenType: TokenTypeTag, col: 15, length: 8},      // "project:" = 8
				{tokenType: TokenTypeTagValue, col: 23, length: 5}, // "alpha" = 5
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens, "expected tokens")

			var tagTokens, valueTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag {
					tagTokens = append(tagTokens, tok)
				}
				if tok.tokenType == TokenTypeTagValue {
					valueTokens = append(valueTokens, tok)
				}
			}

			assert.Len(t, tagTokens, tt.wantTagTokens, "tag token count mismatch")
			assert.Len(t, valueTokens, tt.wantValueTokens, "value token count mismatch")

			if tt.wantPositions != nil {
				allTagTokens := make([]semanticToken, 0)
				for _, tok := range tokens {
					if tok.tokenType == TokenTypeTag || tok.tokenType == TokenTypeTagValue {
						allTagTokens = append(allTagTokens, tok)
					}
				}

				for i, pos := range tt.wantPositions {
					if i < len(allTagTokens) {
						assert.Equal(t, pos.tokenType, allTagTokens[i].tokenType, "token %d type mismatch", i)
						assert.Equal(t, pos.col, allTagTokens[i].col, "token %d column mismatch", i)
						assert.Equal(t, pos.length, allTagTokens[i].length, "token %d length mismatch", i)
					}
				}
			}
		})
	}
}

func TestSemanticTokens_VirtualAccounts(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "balanced virtual account with parentheses",
			content: `2024-01-15 test
    (tracking:virtual)  $100`,
		},
		{
			name: "unbalanced virtual account with brackets",
			content: `2024-01-15 test
    [budget:food]  $-100`,
		},
		{
			name: "mixed regular and virtual accounts",
			content: `2024-01-15 test
    expenses:food  $50
    (tracking:virtual)  $100`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			found := false
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeAccount && tok.modifiers&(1<<ModifierAbstract) != 0 {
					found = true
					break
				}
			}
			assert.True(t, found, "expected namespace token with abstract modifier not found")
		})
	}
}

func TestSemanticTokens_Note(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantPayee    bool
		wantNote     bool
		wantNoteText string
	}{
		{
			name:         "transaction with payee and note",
			content:      "2024-01-15 Whole Foods | Groceries for party",
			wantPayee:    true,
			wantNote:     true,
			wantNoteText: "Groceries for party",
		},
		{
			name:      "transaction with only payee",
			content:   "2024-01-15 Whole Foods",
			wantPayee: true,
			wantNote:  false,
		},
		{
			name:         "transaction with note but no payee",
			content:      "2024-01-15 | Just a note",
			wantPayee:    false,
			wantNote:     true,
			wantNoteText: "Just a note",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			foundPayee := false
			foundNote := false
			for _, tok := range tokens {
				if tok.tokenType == TokenTypePayee {
					foundPayee = true
				}
				if tok.tokenType == TokenTypeNote {
					foundNote = true
				}
			}

			assert.Equal(t, tt.wantPayee, foundPayee, "payee token mismatch")
			assert.Equal(t, tt.wantNote, foundNote, "note token mismatch")
		})
	}
}

func TestSemanticTokens_CompleteExample(t *testing.T) {
	content := `2024-01-15 Whole Foods | Groceries for party
    (tracking:virtual)    $100
    [budget:food]         $-100
    expenses:food          $50
    assets:cash`

	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	var (
		foundDate           bool
		foundPayee          bool
		foundNote           bool
		foundVirtual        int
		foundRegularAccount int
	)

	for _, tok := range tokens {
		if tok.tokenType == TokenTypeDate {
			foundDate = true
		}
		if tok.tokenType == TokenTypePayee {
			foundPayee = true
		}
		if tok.tokenType == TokenTypeNote {
			foundNote = true
		}
		if tok.tokenType == TokenTypeAccount && tok.modifiers&(1<<ModifierAbstract) != 0 {
			foundVirtual++
		}
		if tok.tokenType == TokenTypeAccount && tok.modifiers&(1<<ModifierAbstract) == 0 {
			foundRegularAccount++
		}
	}

	assert.True(t, foundDate, "expected date token")
	assert.True(t, foundPayee, "expected payee token")
	assert.True(t, foundNote, "expected note token")
	assert.Equal(t, 2, foundVirtual, "expected 2 virtual account tokens")
	assert.Equal(t, 2, foundRegularAccount, "expected 2 regular account tokens")
}

func TestSemanticTokens_NoteUsesCommentType(t *testing.T) {
	content := "2024-01-15 Whole Foods | Groceries\n    expenses:food  $50\n    assets:cash"

	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	var noteToken *semanticToken
	for i, tok := range tokens {
		if tok.tokenType == TokenTypeNote {
			noteToken = &tokens[i]
			break
		}
	}

	require.NotNil(t, noteToken, "expected note token after pipe")
	assert.Equal(t, uint32(TokenTypeComment), noteToken.tokenType, "note should use comment type (same index as comment)")
}

func TestSemanticTokens_CyrillicTagPositions(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantPositions []struct {
			tokenType uint32
			col       uint32
			length    uint32
		}
	}{
		{
			name:    "cyrillic tag name with ascii value",
			content: "; клиент:acme",
			// "; " = 2 chars, "клиент:" = 7 chars (6 + colon), "acme" = 4 chars
			// tag col=2, length=7 (UTF-16: клиент=6 + :=1)
			// value col=9, length=4
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 7},
				{tokenType: TokenTypeTagValue, col: 9, length: 4},
			},
		},
		{
			name:    "ascii tag name with cyrillic value",
			content: "; tag:Транспорт",
			// "; " = 2 chars, "tag:" = 4 chars, "Транспорт" = 9 chars
			// tag col=2, length=4
			// value col=6, length=9
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 4},
				{tokenType: TokenTypeTagValue, col: 6, length: 9},
			},
		},
		{
			name:    "cyrillic tag name and value",
			content: "; тег:значение",
			// "; " = 2 chars, "тег:" = 4 chars (3 + colon), "значение" = 8 chars
			// tag col=2, length=4
			// value col=6, length=8
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 4},
				{tokenType: TokenTypeTagValue, col: 6, length: 8},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens, "expected tokens")

			var tagRelatedTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag || tok.tokenType == TokenTypeTagValue {
					tagRelatedTokens = append(tagRelatedTokens, tok)
				}
			}

			require.Len(t, tagRelatedTokens, len(tt.wantPositions), "token count mismatch")

			for i, want := range tt.wantPositions {
				assert.Equal(t, want.tokenType, tagRelatedTokens[i].tokenType, "token %d type", i)
				assert.Equal(t, want.col, tagRelatedTokens[i].col, "token %d col", i)
				assert.Equal(t, want.length, tagRelatedTokens[i].length, "token %d length", i)
			}
		})
	}
}

// Per hledger, a tag value ends only at the next comma or end of line;
// consecutive internal whitespace stays part of the value (hledger-vscode issue #182).
func TestSemanticTokens_TagValueWhitespace(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantPositions []struct {
			tokenType uint32
			col       uint32
			length    uint32
		}
	}{
		{
			name:    "cyrillic tag with double space before amount",
			content: ";  Расходы:Транспорт  71,00",
			// "Расходы:" at col 3, length 8 (7 Cyrillic + colon)
			// value "Транспорт  71" at col 11, length 13 (comma terminates the value)
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 3, length: 8},
				{tokenType: TokenTypeTagValue, col: 11, length: 13},
			},
		},
		{
			name:    "ascii tag value keeps double space",
			content: "; tag:value  extra",
			// "tag:" at col 2, length 4
			// value "value  extra" at col 6, length 12
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 4},
				{tokenType: TokenTypeTagValue, col: 6, length: 12},
			},
		},
		{
			name:    "tag value without double space unchanged",
			content: "; tag:value",
			// "tag:" at col 2, length 4
			// value "value" at col 6, length 5
			wantPositions: []struct {
				tokenType uint32
				col       uint32
				length    uint32
			}{
				{tokenType: TokenTypeTag, col: 2, length: 4},
				{tokenType: TokenTypeTagValue, col: 6, length: 5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens, "expected tokens")

			var tagRelatedTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag || tok.tokenType == TokenTypeTagValue {
					tagRelatedTokens = append(tagRelatedTokens, tok)
				}
			}

			require.Len(t, tagRelatedTokens, len(tt.wantPositions), "token count mismatch")

			for i, want := range tt.wantPositions {
				assert.Equal(t, want.tokenType, tagRelatedTokens[i].tokenType, "token %d type", i)
				assert.Equal(t, want.col, tagRelatedTokens[i].col, "token %d col", i)
				assert.Equal(t, want.length, tagRelatedTokens[i].length, "token %d length", i)
			}
		})
	}
}

func TestSemanticTokens_AccountDirectiveArgType(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantType uint32
	}{
		{
			name:     "account with colon uses namespace type",
			content:  "account expenses:food",
			wantType: TokenTypeAccount,
		},
		{
			name:     "account without colon uses namespace type",
			content:  "account Расходы",
			wantType: TokenTypeAccount,
		},
		{
			name:     "account with colon and cyrillic uses namespace type",
			content:  "account Расходы:Транспорт",
			wantType: TokenTypeAccount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			// Skip the directive token, check the argument token
			var argToken *semanticToken
			for i := range tokens {
				if tokens[i].tokenType != TokenTypeDirective {
					argToken = &tokens[i]
					break
				}
			}
			require.NotNil(t, argToken, "argument token not found")
			assert.Equal(t, tt.wantType, argToken.tokenType,
				"expected token type %d (namespace), got %d", tt.wantType, argToken.tokenType)
			assert.NotZero(t, argToken.modifiers&(1<<ModifierDeclaration),
				"expected declaration modifier")
		})
	}
}

func TestSemanticTokens_CommodityDirectiveArgType(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantType uint32
	}{
		{
			name:     "commodity directive arg uses commodity type",
			content:  "commodity USD",
			wantType: TokenTypeCommodity,
		},
		{
			name:     "commodity directive with symbol uses commodity type",
			content:  "commodity $",
			wantType: TokenTypeCommodity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			var argToken *semanticToken
			for i := range tokens {
				if tokens[i].tokenType != TokenTypeDirective {
					argToken = &tokens[i]
					break
				}
			}
			require.NotNil(t, argToken, "argument token not found")
			assert.Equal(t, tt.wantType, argToken.tokenType,
				"expected commodity type, got %d", argToken.tokenType)
			assert.NotZero(t, argToken.modifiers&(1<<ModifierDeclaration),
				"expected declaration modifier")
		})
	}
}

func TestSemanticTokens_SubdirectiveKeyword(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantKeywords []string
		wantStrings  int
	}{
		{
			name:         "commodity format subdirective",
			content:      "commodity RUB\n    format 1.000,00 RUB",
			wantKeywords: []string{"format"},
			wantStrings:  1,
		},
		{
			name:         "account subdirectives",
			content:      "account expenses\n    alias exp\n    note Expenses\n    type X",
			wantKeywords: []string{"alias", "note", "type"},
			wantStrings:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			var directiveTokens []semanticToken
			var stringTokens []semanticToken
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeDirective && tok.line > 0 {
					directiveTokens = append(directiveTokens, tok)
				}
				if tok.tokenType == TokenTypeString && tok.line > 0 {
					stringTokens = append(stringTokens, tok)
				}
			}

			assert.Len(t, directiveTokens, len(tt.wantKeywords),
				"subdirective keyword count mismatch")
			assert.Len(t, stringTokens, tt.wantStrings,
				"subdirective value count mismatch")
		})
	}
}

func TestSemanticTokens_RulesLegendMapping(t *testing.T) {
	legend := GetSemanticTokensLegend()
	tokenTypes := legend.TokenTypes

	require.True(t, int(TokenTypeRulesKeyword) < len(tokenTypes),
		"TokenTypeRulesKeyword index %d out of legend bounds (%d)", TokenTypeRulesKeyword, len(tokenTypes))
	require.True(t, int(TokenTypeRulesRegexp) < len(tokenTypes),
		"TokenTypeRulesRegexp index %d out of legend bounds (%d)", TokenTypeRulesRegexp, len(tokenTypes))
	require.True(t, int(TokenTypeRulesParameter) < len(tokenTypes),
		"TokenTypeRulesParameter index %d out of legend bounds (%d)", TokenTypeRulesParameter, len(tokenTypes))

	assert.Equal(t, protocol.SemanticTokenKeyword, tokenTypes[TokenTypeDirective])
	assert.Equal(t, protocol.SemanticTokenKeyword, tokenTypes[TokenTypeRulesKeyword])
	assert.Equal(t, protocol.SemanticTokenRegexp, tokenTypes[TokenTypeRulesRegexp])
	assert.Equal(t, protocol.SemanticTokenParameter, tokenTypes[TokenTypeRulesParameter])
}

func TestSemanticTokens_QuotedCommodityLength(t *testing.T) {
	content := "2024-01-15 buy ETF\n    assets:broker  10 \"VWCE\""
	tokens := tokenizeForSemantics(content)

	var commodityToken *semanticToken
	for i := range tokens {
		if tokens[i].tokenType == TokenTypeCommodity && tokens[i].line == 1 {
			commodityToken = &tokens[i]
			break
		}
	}
	require.NotNil(t, commodityToken, "commodity token not found")
	// "VWCE" is 6 characters including quotes
	assert.Equal(t, uint32(6), commodityToken.length,
		"quoted commodity length must include surrounding quotes")
}

func TestSemanticTokens_CommentBlock(t *testing.T) {
	content := `account expenses:food

comment
This is ignored
2024-01-01 Fake transaction
    expenses:food  $50
end comment

2024-01-15 Real transaction
    expenses:food  $30
    assets:cash`

	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	// Collect tokens by line for analysis
	// Lines: 0=account, 1=empty, 2=comment, 3=ignored, 4=ignored, 5=ignored, 6=end comment, 7=empty, 8=real tx...

	// All tokens on lines 3-5 (inside comment block) must be TokenTypeComment
	for _, tok := range tokens {
		if tok.line >= 3 && tok.line <= 5 {
			assert.Equal(t, uint32(TokenTypeComment), tok.tokenType,
				"token on line %d inside comment block should be comment type, got %d", tok.line, tok.tokenType)
		}
	}

	// The "comment" directive on line 2 should be TokenTypeDirective
	var commentDirectiveFound bool
	for _, tok := range tokens {
		if tok.line == 2 && tok.tokenType == TokenTypeDirective {
			commentDirectiveFound = true
			break
		}
	}
	assert.True(t, commentDirectiveFound, "comment directive should be TokenTypeDirective")

	// The "end comment" directive tokens on line 6 should be TokenTypeDirective
	var endDirectiveCount int
	for _, tok := range tokens {
		if tok.line == 6 && tok.tokenType == TokenTypeDirective {
			endDirectiveCount++
		}
	}
	assert.Equal(t, 2, endDirectiveCount, "end comment should produce 2 directive tokens")

	// Code after "end comment" should have normal types
	var foundDateAfter bool
	for _, tok := range tokens {
		if tok.line == 8 && tok.tokenType == TokenTypeDate {
			foundDateAfter = true
			break
		}
	}
	assert.True(t, foundDateAfter, "date after end comment should have normal date type")
}

func TestSemanticTokens_CommentBlockUnterminated(t *testing.T) {
	content := `2024-01-15 Real transaction
    expenses:food  $30
    assets:cash

comment
This is ignored until EOF
2024-02-01 Fake transaction
    expenses:food  $50
`

	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	// All tokens on lines >= 5 (after "comment" directive on line 4) should be TokenTypeComment
	for _, tok := range tokens {
		if tok.line >= 5 {
			assert.Equal(t, uint32(TokenTypeComment), tok.tokenType,
				"token on line %d after unterminated comment should be comment type, got %d", tok.line, tok.tokenType)
		}
	}

	// Tokens before the comment block should have normal types
	var foundDate bool
	for _, tok := range tokens {
		if tok.line == 0 && tok.tokenType == TokenTypeDate {
			foundDate = true
			break
		}
	}
	assert.True(t, foundDate, "date before comment block should have normal date type")
}

func TestSemanticTokens_EndCommentDirective(t *testing.T) {
	// Standalone "end comment" outside a block — both tokens should be directive
	content := "end comment\n"

	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	var directiveTokens []semanticToken
	for _, tok := range tokens {
		if tok.tokenType == TokenTypeDirective {
			directiveTokens = append(directiveTokens, tok)
		}
	}
	require.Len(t, directiveTokens, 2, "end comment should produce 2 directive tokens")
	assert.Equal(t, uint32(0), directiveTokens[0].col, "end token should start at col 0")
	assert.Equal(t, uint32(4), directiveTokens[1].col, "comment token should start at col 4")
}

func TestSemanticTokens_CommentBlockEmpty(t *testing.T) {
	content := `comment
end comment

2024-01-15 test
    expenses:food  $30
    assets:cash`

	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	// No comment-type tokens between comment and end comment (empty block)
	// Date on line 3 should be normal
	var foundDate bool
	for _, tok := range tokens {
		if tok.line == 3 && tok.tokenType == TokenTypeDate {
			foundDate = true
			break
		}
	}
	assert.True(t, foundDate, "date after empty comment block should have normal date type")
}

func TestSemanticTokens_NegativeAmount(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantNegLine uint32 // line where a negative amount token is expected
		wantNeg     bool
	}{
		{
			name:        "negative amount gets negative modifier",
			content:     "2024-01-15 test\n    expenses:food  $-50.00\n    assets:cash",
			wantNegLine: 1,
			wantNeg:     true,
		},
		{
			name:        "leading-sign negative amount",
			content:     "2024-01-15 test\n    expenses:food  -50.00 EUR\n    assets:cash",
			wantNegLine: 1,
			wantNeg:     true,
		},
		{
			name:        "positive amount has no negative modifier",
			content:     "2024-01-15 test\n    expenses:food  $50.00\n    assets:cash",
			wantNegLine: 1,
			wantNeg:     false,
		},
		{
			name:        "explicit plus sign is not negative",
			content:     "2024-01-15 test\n    expenses:food  +50.00 EUR\n    assets:cash",
			wantNegLine: 1,
			wantNeg:     false,
		},
		{
			name:        "negative cyrillic-commodity amount",
			content:     "2024-01-15 тест\n    расходы:еда  -50.00 руб\n    активы:касса",
			wantNegLine: 1,
			wantNeg:     true,
		},
		{
			name:        "CRLF negative amount",
			content:     "2024-01-15 test\r\n    expenses:food  -50.00 EUR\r\n    assets:cash",
			wantNegLine: 1,
			wantNeg:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			foundNegAmount := false
			for _, tok := range tokens {
				if tok.tokenType != TokenTypeAmount {
					continue
				}
				if tok.modifiers&(1<<ModifierNegative) != 0 {
					foundNegAmount = true
				}
			}
			assert.Equal(t, tt.wantNeg, foundNegAmount,
				"negative-modifier presence mismatch on line %d", tt.wantNegLine)
		})
	}
}

func TestSemanticTokens_NegativeModifier_PerAmount(t *testing.T) {
	// Mixed posting with a negative quantity and a positive cost: only the
	// negative quantity must carry the negative modifier.
	content := "2024-01-15 test\n    assets:stock  -10 AAPL @ $5.00\n    assets:cash"
	tokens := tokenizeForSemantics(content)
	require.NotEmpty(t, tokens)

	var negCount, posCount int
	for _, tok := range tokens {
		if tok.tokenType != TokenTypeAmount {
			continue
		}
		if tok.modifiers&(1<<ModifierNegative) != 0 {
			negCount++
		} else {
			posCount++
		}
	}

	// The -10 quantity (sign and digits) is negative; the $5.00 cost is positive.
	assert.GreaterOrEqual(t, negCount, 1, "negative quantity -10 should carry the negative modifier")
	assert.Equal(t, 1, posCount, "the positive cost 5.00 must not carry the negative modifier")
}

// findTokenAt returns the token whose span covers the given (line, col), or nil.
func findTokenAt(tokens []semanticToken, line, col uint32) *semanticToken {
	for i := range tokens {
		t := &tokens[i]
		if t.line == line && col >= t.col && col < t.col+t.length {
			return t
		}
	}
	return nil
}

// A comment that carries tags must still highlight the ';' marker (and any
// surrounding prose) as a comment, so the editor doesn't report "no token" when
// the cursor sits on the semicolon. Regression for issue #28.
func TestSemanticTokens_TaggedCommentMarkerIsComment(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		semicolon   uint32 // column of the ';'
		wantComment []uint32
	}{
		{
			name:        "transaction comment with one tag",
			content:     "2026-01-08 Transfer Yao GB  ; recipent: John",
			semicolon:   28,
			wantComment: []uint32{28, 29}, // ';' and the space before the tag
		},
		{
			name:        "standalone comment with one tag",
			content:     "; client:acme",
			semicolon:   0,
			wantComment: []uint32{0, 1}, // ';' and the leading space
		},
		{
			name:        "trailing prose after comma",
			content:     "; client:acme, more",
			semicolon:   0,
			wantComment: []uint32{0, 1, 13, 15}, // ';', leading space, comma, and trailing prose
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizeForSemantics(tt.content)
			require.NotEmpty(t, tokens)

			marker := findTokenAt(tokens, 0, tt.semicolon)
			require.NotNil(t, marker, "expected a token covering the ';' marker at col %d", tt.semicolon)
			assert.Equal(t, uint32(TokenTypeComment), marker.tokenType,
				"the ';' marker must be a comment token")

			for _, col := range tt.wantComment {
				tok := findTokenAt(tokens, 0, col)
				require.NotNil(t, tok, "expected a token at col %d", col)
				assert.Equal(t, uint32(TokenTypeComment), tok.tokenType,
					"col %d should be a comment token", col)
			}

			// Tag tokens must still be present and correctly typed.
			var sawTag bool
			for _, tok := range tokens {
				if tok.tokenType == TokenTypeTag {
					sawTag = true
				}
			}
			assert.True(t, sawTag, "tag token must still be emitted")
		})
	}
}

func TestSemanticTokens_Legend_NegativeModifier(t *testing.T) {
	legend := GetSemanticTokensLegend()
	assert.Contains(t, legend.TokenModifiers, protocol.SemanticTokenModifiers("negative"))
}
