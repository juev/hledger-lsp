//go:build !wasip1

package stdin

import (
	"io"
	"os"
)

// New returns the server's standard input.
func New() io.Reader {
	return os.Stdin
}
