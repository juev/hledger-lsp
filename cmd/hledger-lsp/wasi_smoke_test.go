package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWASISmoke runs the GOOS=wasip1 build as a stdio language server under
// real WASI runtimes and checks that it answers an editor session the same
// way the native build does. It is opt-in because it needs the runtimes:
//
//	HLEDGER_WASI_RUNTIME=wasmtime,node go test ./cmd/hledger-lsp -run WASI
//
// HLEDGER_WASI_WASM points the test at a prebuilt module (for example the
// release artifact) instead of building one.
func TestWASISmoke(t *testing.T) {
	runtimes := os.Getenv("HLEDGER_WASI_RUNTIME")
	if runtimes == "" {
		t.Skip("set HLEDGER_WASI_RUNTIME=wasmtime,node to run the WASI build under those runtimes")
	}

	wasm := os.Getenv("HLEDGER_WASI_WASM")
	if wasm == "" {
		wasm = buildServer(t, "wasip1", "wasm")
	}
	wasm, err := filepath.Abs(wasm)
	require.NoError(t, err)

	ws := copyWASIWorkspace(t)
	native := buildServer(t, runtime.GOOS, runtime.GOARCH)
	want := runWASISession(t, ws, exec.Command(native))
	checkSessionContent(t, ws, want)
	assert.NotContains(t, want.outside, "outside.journal", "native build reads includes outside the workspace")
	wantUnclean := runUncleanExit(t, ws, exec.Command(native))

	for _, name := range strings.Split(runtimes, ",") {
		t.Run(name, func(t *testing.T) {
			got := runWASISession(t, ws, wasiCommand(t, name, wasm, ws))
			for step, answer := range want.results {
				assert.Equal(t, normalizeOSErrors(answer), normalizeOSErrors(got.results[step]), "step %s differs from the native build", step)
			}
			assert.Empty(t, got.stderr, "stderr")
			assert.Equal(t, 0, got.exitCode, "exit code after shutdown and exit")
			assert.Equal(t, wantUnclean, runUncleanExit(t, ws, wasiCommand(t, name, wasm, ws)), "exit code when stdin closes without shutdown")
			// Only the workspace is preopened, so an include that leaves it
			// is reported as unreadable instead of being read.
			assert.Contains(t, got.outside, `"code":"INCLUDE_NOT_FOUND"`)
			assert.Contains(t, got.outside, "outside.journal")
			// No child processes under WASI: the CLI command fails with a
			// message instead of taking the server down.
			assert.Contains(t, got.cli, `"error"`)
			assert.Contains(t, got.cli, "hledger is not available")
		})
	}
}

// osErrorText matches the operating system's part of a "cannot read included
// file" message. Its wording is the platform's: wasip1 says "No such file or
// directory" where Linux says "no such file or directory".
var osErrorText = regexp.MustCompile(`(cannot read included file: \w+ [^:"]+): [^"]*`)

// serverVersion matches the version in serverInfo: a release module carries
// its release version, the native build made by the test says "dev".
var serverVersion = regexp.MustCompile(`("serverInfo":\{"name":"hledger-lsp","version":)"[^"]*"`)

func normalizeOSErrors(s string) string {
	s = serverVersion.ReplaceAllString(s, `$1"<version>"`)
	return osErrorText.ReplaceAllString(s, "$1: <os error>")
}

func buildServer(t *testing.T, goos, goarch string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "hledger-lsp-"+goos+"-"+goarch)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build for %s/%s: %s", goos, goarch, output)
	return out
}

// copyWASIWorkspace copies the fixture journal into a fresh directory, which
// becomes the only directory the WASI module may access. A journal next to
// it, outside that directory, shows where the sandbox ends.
func copyWASIWorkspace(t *testing.T) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	ws := filepath.Join(parent, "workspace")
	require.NoError(t, os.CopyFS(ws, os.DirFS(filepath.Join("testdata", "wasi", "workspace"))))
	outside := "2024-04-01 outside\n    資産:現金  1.00 RUB\n    доходы:зарплата\n"
	require.NoError(t, os.WriteFile(filepath.Join(parent, "outside.journal"), []byte(outside), 0o644))
	return ws
}

func wasiCommand(t *testing.T, name, wasm, ws string) *exec.Cmd {
	t.Helper()
	switch name {
	case "wasmtime":
		path, err := exec.LookPath("wasmtime")
		require.NoError(t, err, "wasmtime is not on PATH")
		return exec.Command(path, "run", "--dir", ws+"::"+ws, wasm)
	case "node":
		path, err := exec.LookPath("node")
		require.NoError(t, err, "node is not on PATH")
		runner, err := filepath.Abs(filepath.Join("testdata", "wasi", "node-wasi.mjs"))
		require.NoError(t, err)
		return exec.Command(path, "--no-warnings", runner, wasm, ws)
	default:
		t.Fatalf("unknown WASI runtime %q (want wasmtime or node)", name)
		return nil
	}
}

type wasiSession struct {
	results map[string]string
	outside string
	// cli is the answer to hledger.run plus the messages shown meanwhile.
	cli      string
	stderr   string
	exitCode int
}

// runWASISession drives one editor session over the command's stdio. Every
// step's answer is recorded as JSON under the step name, so two builds can be
// compared answer by answer.
func runWASISession(t *testing.T, ws string, cmd *exec.Cmd) wasiSession {
	t.Helper()
	c := startLSPProcess(t, cmd)
	results := map[string]string{}
	wsURI := fileURI(ws)
	mainURI := fileURI(filepath.Join(ws, "main.journal"))

	results["initialize"] = c.call("initialize", map[string]any{
		"processId":        nil,
		"rootUri":          wsURI,
		"workspaceFolders": []any{map[string]any{"uri": wsURI, "name": "workspace"}},
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true},
		},
	})
	c.notify("initialized", map[string]any{})

	disk, err := os.ReadFile(filepath.Join(ws, "main.journal"))
	require.NoError(t, err)
	text := string(disk) + "\n2024-03-02 Несбалансировано\n    расходы:продукты  10.00 RUB\n    資産:現金  -9.00 RUB\n\ninclude nosuch.journal\n"
	// Editors on Windows send CRLF; the server must read it like LF.
	crlf := strings.ReplaceAll(text, "\n", "\r\n")
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": mainURI, "languageId": "hledger", "version": 1, "text": crlf},
	})
	// Nothing else is sent until these arrive: diagnostics are published by a
	// debounce timer, which must fire while the server waits for input.
	results["diagnostics"] = c.waitDiagnostics(mainURI, 1)

	doc := func(line, char int) map[string]any {
		return map[string]any{
			"textDocument": map[string]any{"uri": mainURI},
			"position":     map[string]any{"line": line, "character": char},
		}
	}
	postingLine, _ := positionOf(text, "    расходы:продукты  10.00")
	results["completion"] = c.call("textDocument/completion", doc(postingLine, 4))

	accountLine, accountChar := positionOf(text, "расходы:продукты  1500")
	results["definition"] = c.call("textDocument/definition", doc(accountLine, accountChar+2))

	cashLine, cashChar := positionOf(text, "資産:現金    -1500")
	refs := doc(cashLine, cashChar+1)
	refs["context"] = map[string]any{"includeDeclaration": true}
	results["references"] = c.call("textDocument/references", refs)

	results["documentLink"] = c.call("textDocument/documentLink", map[string]any{"textDocument": map[string]any{"uri": mainURI}})
	results["workspaceSymbol"] = c.call("workspace/symbol", map[string]any{"query": "現金"})
	results["formatting"] = c.call("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
	})
	results["unknownMethod"] = c.call("hledger/noSuchMethod", map[string]any{})

	// An include outside the workspace directory: the native build reads it,
	// a WASI host refuses it because only the workspace is preopened.
	outsideText := text + "include ../outside.journal\n"
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": mainURI, "version": 2},
		"contentChanges": []any{map[string]any{"text": outsideText}},
	})
	outside := c.waitDiagnostics(mainURI, 2)

	// hledger.run starts the hledger CLI as a child process. Native answers
	// depend on whether hledger is installed; WASI has no processes at all.
	cli := c.call("workspace/executeCommand", map[string]any{"command": "hledger.run", "arguments": []any{"bal", mainURI}})

	results["shutdown"] = c.call("shutdown", nil)
	c.notify("exit", nil)
	s := c.finish(t)
	s.results = results
	s.outside = outside
	s.cli = cli + " " + strings.Join(c.shownMessages(), " ")
	return s
}

// runUncleanExit closes stdin right after initialize and returns the exit code.
func runUncleanExit(t *testing.T, ws string, cmd *exec.Cmd) int {
	t.Helper()
	c := startLSPProcess(t, cmd)
	c.call("initialize", map[string]any{"processId": nil, "rootUri": fileURI(ws), "capabilities": map[string]any{}})
	return c.finish(t).exitCode
}

// checkSessionContent pins what the session must show, so that the parity
// comparison cannot pass on two builds that both answer nothing.
func checkSessionContent(t *testing.T, ws string, s wasiSession) {
	t.Helper()
	r := s.results
	assert.Contains(t, r["initialize"], `"name":"hledger-lsp"`)
	assert.Contains(t, r["diagnostics"], "nosuch.journal", "missing include is reported")
	assert.Contains(t, r["diagnostics"], "balance", "unbalanced transaction is reported")
	assert.Contains(t, r["completion"], "расходы:транспорт:метро", "account from a glob include")
	assert.Contains(t, r["completion"], "資産:銀行", "account from an explicit include")
	assert.Contains(t, r["definition"], fileURI(filepath.Join(ws, "accounts.journal")))
	assert.Contains(t, r["references"], fileURI(filepath.Join(ws, "2024", "feb.journal")))
	assert.Contains(t, r["documentLink"], fileURI(filepath.Join(ws, "accounts.journal")))
	assert.Contains(t, r["workspaceSymbol"], "資産:現金")
	assert.Contains(t, r["formatting"], `"newText"`)
	assert.Contains(t, r["unknownMethod"], `"code":-32601`)
	assert.Equal(t, `{"result":null}`, r["shutdown"])
	assert.Empty(t, s.stderr)
	assert.Equal(t, 0, s.exitCode)
}

const wasiStepTimeout = 30 * time.Second

// wasiStartTimeout bounds the first answer, which under wasmtime includes
// compiling the module when it is not cached yet (8.6s on an idle machine).
const wasiStartTimeout = 3 * time.Minute

type lspProcess struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr bytes.Buffer
	nextID int

	writeMu sync.Mutex

	mu          sync.Mutex
	responses   map[int]json.RawMessage
	diagnostics map[string][]json.RawMessage
	messages    []string
	framingErr  error
	readerDone  chan struct{}
	changed     chan struct{}
}

func startLSPProcess(t *testing.T, cmd *exec.Cmd) *lspProcess {
	t.Helper()
	c := &lspProcess{
		t:           t,
		cmd:         cmd,
		responses:   map[int]json.RawMessage{},
		diagnostics: map[string][]json.RawMessage{},
		readerDone:  make(chan struct{}),
		changed:     make(chan struct{}, 1),
	}
	var err error
	c.stdin, err = cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = &c.stderr
	// Bounds how long Wait copies stderr after the process exits, in case a
	// child process still holds it.
	cmd.WaitDelay = time.Second
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	go c.readLoop(stdout)
	return c
}

// readLoop parses everything the server writes to stdout as LSP frames. Any
// byte that is not part of a well-formed frame is a framing error: stdout
// carries the protocol and nothing else.
func (c *lspProcess) readLoop(stdout io.Reader) {
	defer close(c.readerDone)
	r := bufio.NewReader(stdout)
	for {
		body, err := readFrame(r)
		if errors.Is(err, errStreamEnd) {
			return
		}
		if err != nil {
			c.setFramingErr(err)
			_, _ = io.Copy(io.Discard, r)
			return
		}
		var msg struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Params json.RawMessage  `json:"params"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			c.setFramingErr(fmt.Errorf("frame is not JSON: %w: %q", err, body))
			continue
		}
		switch {
		case msg.Method != "" && msg.ID != nil:
			c.answer(*msg.ID, msg.Method)
		case msg.Method == "window/showMessage":
			var p struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			c.mu.Lock()
			c.messages = append(c.messages, p.Message)
			c.mu.Unlock()
		case msg.Method == "textDocument/publishDiagnostics":
			var p struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			c.mu.Lock()
			c.diagnostics[p.URI] = append(c.diagnostics[p.URI], msg.Params)
			c.mu.Unlock()
		case msg.Method == "":
			var id int
			if err := json.Unmarshal(*msg.ID, &id); err == nil {
				c.mu.Lock()
				c.responses[id] = body
				c.mu.Unlock()
			}
		}
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}
}

// errStreamEnd reports that stdout ended between frames. A stream that ends
// inside a frame is a framing error, even though it also wraps io.EOF.
var errStreamEnd = errors.New("stdout ended between frames")

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for first := true; ; first = false {
		line, err := r.ReadString('\n')
		if err != nil {
			if first && line == "" && errors.Is(err, io.EOF) {
				return nil, errStreamEnd
			}
			return nil, fmt.Errorf("truncated header %q: %w", line, err)
		}
		if !strings.HasSuffix(line, "\r\n") {
			return nil, fmt.Errorf("header line without CRLF: %q", line)
		}
		line = strings.TrimSuffix(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, fmt.Errorf("malformed header %q", line)
		}
		if strings.EqualFold(name, "Content-Length") {
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("frame without Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("truncated body: %w", err)
	}
	return body, nil
}

func (c *lspProcess) setFramingErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.framingErr == nil {
		c.framingErr = err
	}
}

// answer replies to requests the server sends to the editor.
func (c *lspProcess) answer(id json.RawMessage, method string) {
	var result any
	if method == "workspace/configuration" {
		result = []any{map[string]any{}}
	}
	// Runs on the reader goroutine, which must not fail the test; a lost
	// answer shows up as a timeout in the step waiting for it.
	_ = c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *lspProcess) write(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func (c *lspProcess) notify(method string, params any) {
	c.t.Helper()
	require.NoError(c.t, c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}))
}

// call sends a request and returns its answer without the envelope: either
// {"result":...} or {"error":...}.
func (c *lspProcess) call(method string, params any) string {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	timeout := wasiStepTimeout
	if id == 1 {
		timeout = wasiStartTimeout
	}
	require.NoError(c.t, c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}))
	var body json.RawMessage
	c.waitFor(method, timeout, func() bool {
		var ok bool
		body, ok = c.responses[id]
		return ok
	})
	var envelope map[string]json.RawMessage
	require.NoError(c.t, json.Unmarshal(body, &envelope))
	delete(envelope, "jsonrpc")
	delete(envelope, "id")
	out, err := json.Marshal(envelope)
	require.NoError(c.t, err)
	return string(out)
}

func (c *lspProcess) shownMessages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.messages)
}

// waitDiagnostics returns the newest diagnostics for uri once one for the
// given document version has arrived.
func (c *lspProcess) waitDiagnostics(uri string, version int) string {
	c.t.Helper()
	var params json.RawMessage
	c.waitFor(fmt.Sprintf("diagnostics for version %d", version), wasiStepTimeout, func() bool {
		for _, p := range c.diagnostics[uri] {
			var d struct {
				Version int `json:"version"`
			}
			_ = json.Unmarshal(p, &d)
			if d.Version == version {
				params = p
				return true
			}
		}
		return false
	})
	return string(params)
}

func (c *lspProcess) waitFor(what string, timeout time.Duration, done func() bool) {
	c.t.Helper()
	deadline := time.After(timeout)
	for {
		c.mu.Lock()
		ok := done()
		framingErr := c.framingErr
		c.mu.Unlock()
		require.NoError(c.t, framingErr, "stdout framing")
		if ok {
			return
		}
		select {
		case <-c.changed:
		case <-c.readerDone:
			c.mu.Lock()
			ok = done()
			c.mu.Unlock()
			require.True(c.t, ok, "server closed stdout while waiting for %s; stderr: %s", what, c.stderr.String())
			return
		case <-deadline:
			c.t.Fatalf("timed out after %s waiting for %s; stderr: %s", timeout, what, c.stderr.String())
		}
	}
}

// finish closes stdin and waits for the server to exit.
func (c *lspProcess) finish(t *testing.T) wasiSession {
	t.Helper()
	require.NoError(t, c.stdin.Close())
	require.NoError(t, c.waitExit(wasiStepTimeout))
	c.mu.Lock()
	defer c.mu.Unlock()
	require.NoError(t, c.framingErr, "stdout framing")
	return wasiSession{exitCode: c.cmd.ProcessState.ExitCode(), stderr: c.stderr.String()}
}

// waitExit waits, within one timeout, for stdout to reach EOF and then for
// the process to exit. Stdout is read to EOF before Wait, which closes the
// pipe. On timeout the process is killed and reaped.
func (c *lspProcess) waitExit(timeout time.Duration) error {
	deadline := time.After(timeout)
	select {
	case <-c.readerDone:
	case <-deadline:
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
		return fmt.Errorf("server did not close stdout within %s", timeout)
	}
	exited := make(chan error, 1)
	go func() { exited <- c.cmd.Wait() }()
	select {
	case err := <-exited:
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return err
		}
		return nil
	case <-deadline:
		_ = c.cmd.Process.Kill()
		<-exited //nolint:errcheck // reap after Kill; the timeout is the error to report
		return fmt.Errorf("server did not exit within %s", timeout)
	}
}

func fileURI(path string) string {
	return "file://" + filepath.ToSlash(path)
}

// positionOf returns the LSP position (line, UTF-16 column) where needle
// starts in text.
func positionOf(text, needle string) (int, int) {
	i := strings.Index(text, needle)
	if i < 0 {
		panic("positionOf: " + needle + " not found")
	}
	before := text[:i]
	line := strings.Count(before, "\n")
	lineStart := strings.LastIndex(before, "\n") + 1
	return line, len(utf16.Encode([]rune(before[lineStart:])))
}
