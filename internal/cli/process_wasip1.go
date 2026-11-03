//go:build wasip1

package cli

// processSupported reports whether this build can start the hledger binary.
// WASI preview 1 has no way to spawn a process.
const processSupported = false
