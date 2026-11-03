// Command hledger-fmt formats hledger journal files without a running language
// server. It applies the same formatter as textDocument/formatting.
package main

import (
	"fmt"
	"os"

	"github.com/juev/hledger-lsp/internal/fmtcmd"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("hledger-fmt %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	os.Exit(fmtcmd.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
