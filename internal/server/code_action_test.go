package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lsp.dev/protocol"

	"github.com/juev/hledger-lsp/internal/cli"
)

// fakeCLIClient is a cliRunner that records the file path it was asked to run
// against, so tests can assert which journal a command targeted without the
// real hledger binary.
type fakeCLIClient struct {
	available bool
	lastFile  string
}

func (f *fakeCLIClient) Available() bool { return f.available }

func (f *fakeCLIClient) Run(_ context.Context, file string, _ ...string) (string, error) {
	f.lastFile = file
	return "", nil
}

func TestFormatOutputAsComment(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		output   string
		expected string
	}{
		{
			name:   "simple balance output",
			cmd:    "bal",
			output: "         $100.00  assets\n        -$100.00  expenses",
			expected: `; === hledger bal ===
;          $100.00  assets
;         -$100.00  expenses
; ==================`,
		},
		{
			name:   "empty output",
			cmd:    "accounts",
			output: "",
			expected: `; === hledger accounts ===
; (no output)
; =======================`,
		},
		{
			name:   "multiline register",
			cmd:    "reg",
			output: "2024-01-15 Grocery     expenses:food    $50.00    $50.00\n                      assets:bank     -$50.00         0",
			expected: `; === hledger reg ===
; 2024-01-15 Grocery     expenses:food    $50.00    $50.00
;                       assets:bank     -$50.00         0
; ==================`,
		},
		{
			name:   "trailing newlines stripped",
			cmd:    "bal",
			output: "  $100  assets\n\n\n",
			expected: `; === hledger bal ===
;   $100  assets
; ==================`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatOutputAsComment(tt.cmd, tt.output)
			if result != tt.expected {
				t.Errorf("formatOutputAsComment(%q, %q):\ngot:\n%s\n\nwant:\n%s", tt.cmd, tt.output, result, tt.expected)
			}
		})
	}
}

func TestGetHledgerCommands(t *testing.T) {
	commands := getHledgerCommands()

	expectedCmds := []string{"bal", "reg", "is", "bs", "cf"}
	if len(commands) != len(expectedCmds) {
		t.Errorf("expected %d commands, got %d", len(expectedCmds), len(commands))
	}

	for _, cmd := range expectedCmds {
		found := false
		for _, c := range commands {
			if c.cmd == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected command %q not found", cmd)
		}
	}
}

func TestServer_CodeAction_WithoutCLI(t *testing.T) {
	s := &Server{
		cliClient: nil,
	}

	actions := s.getCodeActions("file:///test.journal")
	if len(actions) != 0 {
		t.Errorf("expected no actions without CLI client, got %d", len(actions))
	}
}

func TestServer_CodeAction_CLINotAvailable(t *testing.T) {
	s := &Server{
		cliClient: cli.NewClient("/nonexistent/hledger", 5*time.Second),
	}

	actions := s.getCodeActions("file:///test.journal")
	if len(actions) != 0 {
		t.Errorf("expected no actions with unavailable CLI, got %d", len(actions))
	}
}

func TestServer_CodeAction_WithCLI(t *testing.T) {
	s := NewServer()
	s.cliClient = cli.NewClient("hledger", 5*time.Second)

	if !s.cliClient.Available() {
		t.Skip("hledger not available")
	}

	actions := s.getCodeActions("file:///test.journal")
	if len(actions) == 0 {
		t.Error("expected code actions with CLI client")
	}

	for _, action := range actions {
		if action.Title == "" {
			t.Error("action title should not be empty")
		}
		if action.Kind != "source.hledger" {
			t.Errorf("action kind = %q; want %q", action.Kind, "source.hledger")
		}
	}
}

func TestCommentLinePrefix(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"hello", "; hello"},
		{"  indented", ";   indented"},
		{"", "; "},
		{"   ", ";    "},
	}

	for _, tt := range tests {
		result := "; " + tt.line
		if result != tt.expected {
			t.Errorf("comment prefix for %q: got %q, want %q", tt.line, result, tt.expected)
		}
	}
}

func TestHeaderLine(t *testing.T) {
	tests := []struct {
		cmd      string
		expected string
	}{
		{"bal", "; === hledger bal ==="},
		{"register", "; === hledger register ==="},
	}

	for _, tt := range tests {
		result := "; === hledger " + tt.cmd + " ==="
		if result != tt.expected {
			t.Errorf("header for %q: got %q, want %q", tt.cmd, result, tt.expected)
		}
	}
}

func TestFooterLine(t *testing.T) {
	header := "; === hledger bal ==="
	footer := "; " + strings.Repeat("=", len(header)-2)

	if len(footer) != len(header) {
		t.Errorf("footer length (%d) should match header length (%d)", len(footer), len(header))
	}
}

func TestGetCodeActions_EmbedsDocumentURI(t *testing.T) {
	s := NewServer()
	s.cliClient = &fakeCLIClient{available: true}

	docURI := protocol.DocumentURI("file:///test.journal")
	actions := s.getCodeActions(docURI)

	require.NotEmpty(t, actions)
	for _, action := range actions {
		require.NotNil(t, action.Command)
		require.Len(t, action.Command.Arguments, 2, "command must carry the invoking document URI")
		assert.Equal(t, string(docURI), action.Command.Arguments[1])
	}
}

func TestExecuteCommand_TargetsInvokingURI(t *testing.T) {
	s := NewServer()
	fake := &fakeCLIClient{available: true}
	s.cliClient = fake

	uriA := protocol.DocumentURI("file:///a.journal")
	uriB := protocol.DocumentURI("file:///b.journal")
	s.documents.Store(uriA, "2024-01-01 a\n    expenses  $1\n    assets\n")
	s.documents.Store(uriB, "2024-01-01 b\n    expenses  $2\n    assets\n")

	params := &protocol.ExecuteCommandParams{
		Command:   "hledger.run",
		Arguments: []any{"bal", string(uriB)},
	}

	// The command must target the invoking document (B). Before the fix the
	// handler ranged over the open documents in arbitrary order, so repeat to
	// surface that nondeterminism reliably.
	for i := 0; i < 20; i++ {
		_, err := s.ExecuteCommand(context.Background(), params)
		require.NoError(t, err)
		assert.Equal(t, uriToPath(uriB), fake.lastFile)
	}
}

func TestExecuteCommand_FallbackWithoutURI(t *testing.T) {
	s := NewServer()
	fake := &fakeCLIClient{available: true}
	s.cliClient = fake

	uriA := protocol.DocumentURI("file:///a.journal")
	s.documents.Store(uriA, "2024-01-01 a\n    expenses  $1\n    assets\n")

	params := &protocol.ExecuteCommandParams{
		Command:   "hledger.run",
		Arguments: []any{"bal"},
	}

	_, err := s.ExecuteCommand(context.Background(), params)
	require.NoError(t, err)
	assert.Equal(t, uriToPath(uriA), fake.lastFile)
}
