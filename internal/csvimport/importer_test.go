package csvimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests here stand a shell script in for hledger to inject failures. They
// check that each failure is reported, not what hledger would convert.

const validJSON = `[{"tindex":1,"tdate":"2024-01-02","tdescription":"d","tsourcepos":[{"sourceName":"DATA","sourceLine":2},{"sourceName":"DATA","sourceLine":3}],
 "tpostings":[{"paccount":"a","preal":"RealPosting","pbalanceassertion":null,"pamount":[{"acommodity":"$","acost":null,"acostbasis":null,"aquantity":{"decimalMantissa":150,"decimalPlaces":2},"astyle":{"ascommodityside":"L"}}]},
              {"paccount":"b","preal":"RealPosting","pbalanceassertion":null,"pamount":[{"acommodity":"$","acost":null,"acostbasis":null,"aquantity":{"decimalMantissa":-150,"decimalPlaces":2},"astyle":{"ascommodityside":"L"}}]}]}]`

const validText = "2024-01-02 d\n    a  $1.50\n    b\n"

type fakeRun struct {
	version string
	json    string
	text    string
	script  string // replaces the print behaviour entirely when set
}

// fakeHledger writes a script answering --version and the two print runs.
func fakeHledger(t *testing.T, dir string, run fakeRun) string {
	t.Helper()
	if run.version == "" {
		run.version = "1.99.5"
	}
	write(t, dir, "fake/out.json", run.json)
	write(t, dir, "fake/out.txt", run.text)
	body := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'hledger " + run.version + ", linux-x86_64'; exit 0; fi\n"
	if run.script != "" {
		body += run.script
	} else {
		body += "case \"$*\" in *json*) cat " + filepath.Join(dir, "fake/out.json") + ";; *) cat " + filepath.Join(dir, "fake/out.txt") + ";; esac\n"
	}
	path := write(t, dir, "fake/hledger", body)
	require.NoError(t, os.Chmod(path, 0o700))
	return path
}

func fixture(t *testing.T) (dir, data string) {
	t.Helper()
	dir = t.TempDir()
	data = write(t, dir, "in/bank.csv", "date,amount\n2024-01-02,1.50\n")
	write(t, dir, "in/bank.csv.rules", "skip 1\nfields date, amount\naccount1 a\naccount2 b\n")
	return dir, data
}

func runFake(t *testing.T, binary, data string, timeout time.Duration) Result {
	t.Helper()
	importer := New(Config{Enabled: true, Path: binary, Timeout: timeout})
	return importer.Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})
}

func TestImport_ValidOutputConverts(t *testing.T) {
	dir, data := fixture(t)
	binary := fakeHledger(t, dir, fakeRun{json: strings.ReplaceAll(validJSON, "DATA", data), text: validText})

	result := runFake(t, binary, data, 5*time.Second)

	require.Empty(t, result.Err)
	require.Len(t, result.Transactions, 1)
	assert.Equal(t, "1.5", result.Transactions[0].Postings[0].Amount.Quantity.String())
	assert.Nil(t, result.Transactions[0].Postings[1].Amount, "an amount the text leaves implicit stays implicit")
}

func TestImport_FailuresAreExplicit(t *testing.T) {
	cases := []struct {
		name    string
		run     fakeRun
		timeout time.Duration
		want    string
	}{
		{"malformed JSON", fakeRun{json: "{not json", text: validText}, 0, "cannot read hledger's JSON output"},
		{"truncated JSON", fakeRun{json: validJSON[:len(validJSON)/2], text: validText}, 0, "cannot read hledger's JSON output"},
		{"trailing data", fakeRun{json: validJSON + "[]", text: validText}, 0, "unexpected data after the transactions"},
		{"JSON null", fakeRun{json: "null", text: validText}, 0, "no transaction list"},
		{"count mismatch", fakeRun{json: validJSON, text: validText + "\n" + validText}, 0, "hledger printed 2 transactions as text and 1 as JSON"},
		{"account mismatch", fakeRun{json: validJSON, text: strings.Replace(validText, "    a ", "    z ", 1)}, 0, `the printed account "z" differs from hledger's`},
		{"unparsable text", fakeRun{json: validJSON, text: "2024-13-45 x\n    a  1\n"}, 0, "cannot read hledger's print output"},
		{"foreign data file", fakeRun{json: strings.ReplaceAll(validJSON, "DATA", "/elsewhere/x.csv"), text: validText}, 0, "hledger read x.csv, which the import did not check"},
		{"huge mantissa", fakeRun{json: strings.Replace(validJSON, `"decimalMantissa":150`, `"decimalMantissa":`+strings.Repeat("9", 400), 1), text: validText}, 0, "unrepresentable: quantity mantissa"},
		{"bad places", fakeRun{json: strings.Replace(validJSON, `"decimalPlaces":2}`, `"decimalPlaces":300}`, 1), text: validText}, 0, `unrepresentable: "300" decimal places`},
		{"two commodities", fakeRun{json: strings.Replace(validJSON, `"pamount":[{`, `"pamount":[{"acommodity":"€","aquantity":{"decimalMantissa":1,"decimalPlaces":0}},{`, 1), text: validText}, 0, "an amount in 2 commodities"},
		{"lot cost basis", fakeRun{json: strings.Replace(validJSON, `"acostbasis":null`, `"acostbasis":{"cbCost":null}`, 1), text: validText}, 0, "a lot cost basis"},
		{"nonzero exit", fakeRun{script: "echo 'hledger: Error: bad\x01 rules' >&2\nexit 1\n"}, 0, "hledger could not convert the file: bad rules"},
		{"output limit", fakeRun{script: "while :; do echo 0123456789012345678901234567890123456789; done\n"}, 0, "hledger's output exceeds the import limit"},
		{"timeout", fakeRun{script: "exec sleep 30\n"}, 300 * time.Millisecond, "hledger did not finish within hledger.cli.timeout"},
		{"old version", fakeRun{version: "1.52.4", json: validJSON, text: validText}, 0, "hledger 1.52.4 cannot include CSV files; hledger 1.99.5 or newer is needed"},
		{"older preview", fakeRun{version: "1.99.4", json: validJSON, text: validText}, 0, "hledger 1.99.4 cannot include CSV files"},
		{"unknown version", fakeRun{version: "x", json: validJSON, text: validText}, 0, "hledger of unknown version cannot include CSV files"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, data := fixture(t)
			tc.run.json = strings.ReplaceAll(tc.run.json, "DATA", data)
			binary := fakeHledger(t, dir, tc.run)
			timeout := tc.timeout
			if timeout == 0 {
				timeout = 5 * time.Second
			}
			start := time.Now()
			result := runFake(t, binary, data, timeout)
			assert.Contains(t, result.Err, tc.want)
			assert.Empty(t, result.Transactions)
			assert.Equal(t, []string{data, data + ".rules"}, result.Sources)
			assert.Less(t, time.Since(start), 10*time.Second)
		})
	}
}

func TestImport_LongStderrIsClipped(t *testing.T) {
	dir, data := fixture(t)
	binary := fakeHledger(t, dir, fakeRun{script: "i=0; while [ $i -lt 400 ]; do printf 'Ошибка ' >&2; i=$((i+1)); done\nexit 1\n"})
	result := runFake(t, binary, data, 5*time.Second)
	assert.LessOrEqual(t, len(result.Err), len("hledger could not convert the file: ")+maxMessage+len("…"))
	assert.True(t, strings.HasSuffix(result.Err, "…"))
}

func TestImport_ConfigurationFailures(t *testing.T) {
	dir, data := fixture(t)
	binary := fakeHledger(t, dir, fakeRun{json: strings.ReplaceAll(validJSON, "DATA", data), text: validText})
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20}

	disabled := New(Config{Enabled: false, Path: binary, Timeout: time.Second}).Import(context.Background(), request)
	assert.Equal(t, "including CSV files needs the hledger CLI, which is disabled (hledger.cli.enabled is false)", disabled.Err)
	assert.Equal(t, []string{data, data + ".rules"}, disabled.Sources, "sources are known without the CLI")

	missing := New(Config{Enabled: true, Path: filepath.Join(dir, "no-such-hledger"), Timeout: time.Second}).Import(context.Background(), request)
	assert.Contains(t, missing.Err, "which is not available at")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := New(Config{Enabled: true, Path: binary, Timeout: time.Second}).Import(ctx, request)
	assert.Equal(t, "the import was cancelled", cancelled.Err)
}

func TestImport_CancellationIsNotCached(t *testing.T) {
	dir, data := fixture(t)
	marker := filepath.Join(dir, "slow")
	write(t, dir, "slow", "")
	binary := fakeHledger(t, dir, fakeRun{script: "if [ -e " + marker + " ]; then exec sleep 30; fi\ncase \"$*\" in *json*) cat " + filepath.Join(dir, "fake/out.json") + ";; *) cat " + filepath.Join(dir, "fake/out.txt") + ";; esac\n", json: strings.ReplaceAll(validJSON, "DATA", data), text: validText})
	importer := New(Config{Enabled: true, Path: binary, Timeout: 30 * time.Second})
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	first := importer.Import(ctx, request)
	assert.Equal(t, "the import was cancelled", first.Err)

	require.NoError(t, os.Remove(marker))
	second := importer.Import(context.Background(), request)
	require.Empty(t, second.Err, "a cancelled run must not be served from the cache")
	assert.Len(t, second.Transactions, 1)
}

func TestImport_ChangeDuringTheRunIsRetried(t *testing.T) {
	dir, data := fixture(t)
	// The first JSON run rewrites the data file, as an editor saving in the
	// middle of a conversion would.
	once := filepath.Join(dir, "once")
	binary := fakeHledger(t, dir, fakeRun{
		json: strings.ReplaceAll(validJSON, "DATA", data), text: validText,
		script: "case \"$*\" in *json*) if [ ! -e " + once + " ]; then : > " + once + "; printf 'date,amount\\n2024-01-02,1.50\\n\\n' > " + data + "; fi; cat " + filepath.Join(dir, "fake/out.json") + ";; *) cat " + filepath.Join(dir, "fake/out.txt") + ";; esac\n",
	})
	importer := New(Config{Enabled: true, Path: binary, Timeout: 5 * time.Second})

	result := importer.Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})

	require.Empty(t, result.Err)
	assert.Len(t, result.Transactions, 1)
	_, err := os.Stat(once)
	require.NoError(t, err, "the first run did change the file")
}

func TestImport_UnsavedBufferIsReported(t *testing.T) {
	dir, data := fixture(t)
	binary := fakeHledger(t, dir, fakeRun{json: strings.ReplaceAll(validJSON, "DATA", data), text: validText})
	importer := New(Config{Enabled: true, Path: binary, Timeout: 5 * time.Second})
	buffers := map[string]string{
		data:            "date,amount\r\n2024-01-02,1.50\r\n", // same text, other line endings
		data + ".rules": "skip 1\nfields date, amount\naccount1 changed\naccount2 b\n",
	}
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20, Buffer: func(path string) (string, bool) {
		content, ok := buffers[path]
		return content, ok
	}}

	result := importer.Import(context.Background(), request)

	require.Empty(t, result.Err)
	assert.Equal(t, []string{"bank.csv.rules has unsaved changes; the import reads the saved file"}, result.Warnings)
	assert.Len(t, result.Transactions, 1, "the saved files are still imported")

	cached := importer.Import(context.Background(), request)
	assert.Equal(t, result.Warnings, cached.Warnings, "a cached conversion still reports the buffer")
}

func TestImport_ReaderSelection(t *testing.T) {
	cases := []struct {
		argument string
		reader   Reader
		rest     string
	}{
		{"csv:bank.txt", ReaderCSV, "bank.txt"},
		{"ssv:x.journal", ReaderSSV, "x.journal"},
		{"tsv:/abs/x", ReaderTSV, "/abs/x"},
		{"rules:x.rules", "", "rules:x.rules"},
		{"journal:x.csv", "", "journal:x.csv"},
		{"csv:", "", "csv:"},
		{"C:/x.csv", "", "C:/x.csv"},
		{"bank.csv", "", "bank.csv"},
	}
	for _, tc := range cases {
		reader, rest := SplitReader(tc.argument)
		assert.Equal(t, tc.reader, reader, tc.argument)
		assert.Equal(t, tc.rest, rest, tc.argument)
	}
	assert.Equal(t, ReaderCSV, ReaderForPath("/x/Bank.CSV"))
	assert.Equal(t, ReaderRules, ReaderForPath("/x/bank.csv.rules"))
	assert.Equal(t, Reader(""), ReaderForPath("/x/bank.journal"))
	assert.True(t, IsDataPath("a.tsv"))
	assert.False(t, IsDataPath("a.rules"))
}

func TestImport_StaticChecksRejectNonRegularAndOversizedInput(t *testing.T) {
	dir := t.TempDir()
	data := write(t, dir, "big.csv", strings.Repeat("x", 100))
	write(t, dir, "big.csv.rules", "fields a\n")
	binary := fakeHledger(t, dir, fakeRun{json: "[]", text: ""})
	importer := New(Config{Enabled: true, Path: binary, Timeout: 5 * time.Second})

	big := importer.Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 10})
	assert.Equal(t, "data file big.csv is too large: 100 bytes (max 10)", big.Err)

	directory := filepath.Join(dir, "dir.csv")
	require.NoError(t, os.Mkdir(directory, 0o755))
	notRegular := importer.Import(context.Background(), Request{Reader: ReaderCSV, Path: directory, MaxFileSize: 1 << 20})
	assert.Equal(t, "data file dir.csv is not a regular file", notRegular.Err)

	var repeated strings.Builder
	repeated.WriteString("source ./big.csv\n")
	for range maxRulesIncludes + 1 {
		repeated.WriteString("include leaf.rules\n")
	}
	write(t, dir, "leaf.rules", "# leaf\n")
	rules := write(t, dir, "many.rules", repeated.String())
	expanded := importer.Import(context.Background(), Request{Reader: ReaderRules, Path: rules, MaxFileSize: 1 << 20})
	assert.Equal(t, "rules files include more than 256 files", expanded.Err)
}

// A conversion whose inputs change while hledger runs must not reach the cache
// or the callers waiting on it. The stand-in reads the amount from the data
// file, then blocks until the test has changed the file, so the output is
// known to describe the old bytes.
func TestImport_StaleConversionIsNeverServed(t *testing.T) {
	dir, data := fixture(t)
	write(t, dir, "in/bank.csv", "date,amount\n2024-01-02,150\n")
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	json := strings.Replace(strings.ReplaceAll(validJSON, "DATA", data), `"decimalMantissa":150`, `"decimalMantissa":MANTISSA`, 1)
	write(t, dir, "fake/template.json", json)
	script := "case \"$*\" in *json*)\n" +
		"  m=$(sed -n 2p " + data + " | cut -d, -f2)\n" +
		"  if [ ! -e " + release + " ]; then : > " + started + "; while [ ! -e " + release + " ]; do sleep 0.02; done; fi\n" +
		"  sed \"s/MANTISSA/$m/\" " + filepath.Join(dir, "fake/template.json") + ";;\n" +
		"*) cat " + filepath.Join(dir, "fake/out.txt") + ";;\nesac\n"
	binary := fakeHledger(t, dir, fakeRun{text: validText, script: script})
	importer := New(Config{Enabled: true, Path: binary, Timeout: 30 * time.Second})
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20}

	results := make(chan Result, 2)
	go func() { results <- importer.Import(context.Background(), request) }()
	require.Eventually(t, func() bool { _, err := os.Stat(started); return err == nil }, 10*time.Second, 10*time.Millisecond)
	// The second caller plans the old bytes too and waits on the running
	// conversion.
	go func() { results <- importer.Import(context.Background(), request) }()
	require.Eventually(t, func() bool {
		importer.mu.Lock()
		defer importer.mu.Unlock()
		return len(importer.inflight) == 1
	}, 10*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	write(t, dir, "in/bank.csv", "date,amount\n2024-01-02,275\n")
	write(t, dir, "release", "")

	for range 2 {
		result := <-results
		require.Empty(t, result.Err)
		require.Len(t, result.Transactions, 1)
		assert.Equal(t, "2.75", result.Transactions[0].Postings[0].Amount.Quantity.String(), "no caller may see the conversion of the old bytes")
	}
	again := importer.Import(context.Background(), request)
	assert.Equal(t, "2.75", again.Transactions[0].Postings[0].Amount.Quantity.String())
}
