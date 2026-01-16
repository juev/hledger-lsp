package server

import (
	"context"
	"fmt"
	"strings"

	"go.lsp.dev/protocol"
)

type hledgerCommand struct {
	cmd   string
	title string
}

func getHledgerCommands() []hledgerCommand {
	return []hledgerCommand{
		{cmd: "bal", title: "Run hledger bal (balance)"},
		{cmd: "reg", title: "Run hledger reg (register)"},
		{cmd: "is", title: "Run hledger is (income statement)"},
		{cmd: "bs", title: "Run hledger bs (balance sheet)"},
		{cmd: "cf", title: "Run hledger cf (cash flow)"},
	}
}

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	actions := s.getCodeActions()

	result := make([]protocol.CodeAction, 0, len(actions))
	for _, action := range actions {
		a := action
		a.Diagnostics = nil
		result = append(result, a)
	}

	return result, nil
}

func (s *Server) getCodeActions() []protocol.CodeAction {
	settings := s.getSettings()
	if s.cliClient == nil || !s.cliClient.Available() || !settings.CLI.Enabled {
		return nil
	}

	commands := getHledgerCommands()
	actions := make([]protocol.CodeAction, 0, len(commands))

	for _, cmd := range commands {
		actions = append(actions, protocol.CodeAction{
			Title: cmd.title,
			Kind:  "source.hledger",
			Command: &protocol.Command{
				Title:   cmd.title,
				Command: "hledger.run",
				Arguments: []any{
					cmd.cmd,
				},
			},
		})
	}

	return actions
}

func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if params.Command != "hledger.run" {
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}

	if len(params.Arguments) < 1 {
		return nil, fmt.Errorf("missing command argument")
	}

	cmd, ok := params.Arguments[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid command argument type")
	}

	if s.cliClient == nil || !s.cliClient.Available() {
		return nil, fmt.Errorf("hledger not available")
	}

	var filePath string
	s.documents.Range(func(key, _ any) bool {
		docURI := key.(protocol.DocumentURI)
		path := uriToPath(docURI)
		if path != "" {
			filePath = path
			return false
		}
		return true
	})

	if filePath == "" {
		return nil, fmt.Errorf("no document open")
	}

	output, err := s.cliClient.Run(ctx, filePath, cmd)
	if err != nil {
		return formatOutputAsComment(cmd, fmt.Sprintf("Error: %v", err)), nil
	}

	return formatOutputAsComment(cmd, output), nil
}

func formatOutputAsComment(cmd, output string) string {
	header := fmt.Sprintf("; === hledger %s ===", cmd)
	footer := "; " + strings.Repeat("=", len(header)-3)

	output = strings.TrimRight(output, "\n\r\t ")

	var lines []string
	if output == "" {
		lines = []string{"(no output)"}
	} else {
		lines = strings.Split(output, "\n")
	}

	var builder strings.Builder
	builder.WriteString(header)
	builder.WriteString("\n")

	for _, line := range lines {
		builder.WriteString("; ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}

	builder.WriteString(footer)

	return builder.String()
}
