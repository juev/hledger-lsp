package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/juev/hledger-lsp/internal/testutil"
)

// TestWASIMeasure prints startup time, request latency, peak memory and idle
// CPU of the native and WASI builds on two journals: the smoke-test fixture
// and a generated include tree of 10 files with 1000 transactions each.
// Linux only (it reads /proc), opt-in, and it asserts nothing:
//
//	HLEDGER_WASI_MEASURE=native,wasmtime,node go test ./cmd/hledger-lsp -run WASIMeasure -v
func TestWASIMeasure(t *testing.T) {
	targets := os.Getenv("HLEDGER_WASI_MEASURE")
	if targets == "" {
		t.Skip("set HLEDGER_WASI_MEASURE=native,wasmtime,node to measure those builds")
	}
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}

	wasm := os.Getenv("HLEDGER_WASI_WASM")
	if wasm == "" {
		wasm = buildServer(t, "wasip1", "wasm")
	}
	wasm, err := filepath.Abs(wasm)
	require.NoError(t, err)
	native := buildServer(t, runtime.GOOS, runtime.GOARCH)

	fixture := copyWASIWorkspace(t)
	large := filepath.Join(t.TempDir(), "large")
	require.NoError(t, os.Mkdir(large, 0o755))
	_, err = testutil.GenerateIncludeTree(large, 10, 1000)
	require.NoError(t, err)

	t.Logf("| journal | build | startup | first diagnostics | completion p50 / max | formatting p50 | edit to diagnostics p50 | peak RSS | idle CPU |")
	t.Logf("| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, ws := range []struct{ name, dir string }{{"fixture (4 files)", fixture}, {"10 files x 1000 tx", large}} {
		for _, target := range strings.Split(targets, ",") {
			var cmd *exec.Cmd
			if target == "native" {
				cmd = exec.Command(native)
			} else {
				cmd = wasiCommand(t, target, wasm, ws.dir)
			}
			t.Logf("| %s | %s | %s |", ws.name, target, measureSession(t, ws.dir, cmd))
		}
	}
}

func measureSession(t *testing.T, ws string, cmd *exec.Cmd) string {
	t.Helper()
	start := time.Now()
	c := startLSPProcess(t, cmd)
	wsURI := fileURI(ws)
	mainURI := fileURI(filepath.Join(ws, "main.journal"))
	c.call("initialize", map[string]any{
		"processId":        nil,
		"rootUri":          wsURI,
		"workspaceFolders": []any{map[string]any{"uri": wsURI, "name": "workspace"}},
		"capabilities":     map[string]any{},
	})
	startup := time.Since(start)
	c.notify("initialized", map[string]any{})

	disk, err := os.ReadFile(filepath.Join(ws, "main.journal"))
	require.NoError(t, err)
	text := string(disk) + "\n2024-12-31 measured\n    expenses:food  10.00 RUB\n    assets:cash\n"
	start = time.Now()
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": mainURI, "languageId": "hledger", "version": 1, "text": text},
	})
	c.waitDiagnostics(mainURI, 1)
	firstDiagnostics := time.Since(start)

	postingLine, _ := positionOf(text, "    assets:cash\n")
	completion := timeCalls(20, func() {
		c.call("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": postingLine, "character": 4},
		})
	})
	formatting := timeCalls(5, func() {
		c.call("textDocument/formatting", map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
		})
	})
	version := 1
	edit := timeCalls(5, func() {
		version++
		text += fmt.Sprintf("\n2024-12-31 edit %d\n    expenses:food  1.00 RUB\n    assets:cash\n", version)
		c.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": mainURI, "version": version},
			"contentChanges": []any{map[string]any{"text": text}},
		})
		c.waitDiagnostics(mainURI, version)
	})

	pid := c.cmd.Process.Pid
	cpuBefore := processCPU(t, pid)
	idle := 3 * time.Second
	time.Sleep(idle)
	idleCPU := processCPU(t, pid) - cpuBefore
	peak := peakRSS(t, pid)

	c.call("shutdown", nil)
	c.notify("exit", nil)
	c.finish(t)

	return fmt.Sprintf("%s | %s | %s / %s | %s | %s | %d MiB | %.1f%%",
		ms(startup), ms(firstDiagnostics), ms(completion[len(completion)/2]), ms(completion[len(completion)-1]),
		ms(formatting[len(formatting)/2]), ms(edit[len(edit)/2]), peak>>20, 100*idleCPU.Seconds()/idle.Seconds())
}

// timeCalls runs f n times and returns the sorted durations.
func timeCalls(n int, f func()) []time.Duration {
	d := make([]time.Duration, n)
	for i := range d {
		start := time.Now()
		f()
		d[i] = time.Since(start)
	}
	slices.Sort(d)
	return d
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000)
}

// processCPU returns user plus system CPU time of pid from /proc/pid/stat,
// assuming the usual 100 clock ticks per second.
func processCPU(t *testing.T, pid int) time.Duration {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	require.NoError(t, err)
	// Fields after the parenthesised command name; utime and stime are the
	// 14th and 15th fields overall.
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+2:]))
	utime, err := strconv.ParseInt(fields[11], 10, 64)
	require.NoError(t, err)
	stime, err := strconv.ParseInt(fields[12], 10, 64)
	require.NoError(t, err)
	return time.Duration(utime+stime) * 10 * time.Millisecond
}

// peakRSS returns VmHWM of pid in bytes.
func peakRSS(t *testing.T, pid int) int64 {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			require.NoError(t, err)
			return kb << 10
		}
	}
	t.Fatal("no VmHWM in /proc status")
	return 0
}
