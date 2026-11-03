package csvimport

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowValidHledger answers like hledger after a pause, so a caller that waited
// for it would be seen waiting.
func slowValidHledger(t *testing.T, dir, data string, pause time.Duration) string {
	t.Helper()
	return fakeHledger(t, dir, fakeRun{
		json: strings.ReplaceAll(validJSON, "DATA", data),
		text: validText,
		script: "sleep " + strconv.FormatFloat(pause.Seconds(), 'f', -1, 64) + "\n" +
			"case \"$*\" in *json*) cat " + filepath.Join(dir, "fake/out.json") + ";; *) cat " + filepath.Join(dir, "fake/out.txt") + ";; esac\n",
	})
}

// Under Background a conversion that is not cached never holds the caller: it
// is reported pending at once, OnReady says when it is done, and the next
// Import gets it from the cache.
func TestImport_BackgroundReportsPendingAndThenTheConversion(t *testing.T) {
	dir, data := fixture(t)
	binary := slowValidHledger(t, dir, data, 1*time.Second)
	ready := make(chan struct{}, 4)
	var calls atomic.Int32
	importer := New(Config{Enabled: true, Path: binary, Timeout: 10 * time.Second, Background: true, OnReady: func() {
		calls.Add(1)
		ready <- struct{}{}
	}})
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20}

	start := time.Now()
	first := importer.Import(context.Background(), request)
	second := importer.Import(context.Background(), request)
	assert.Less(t, time.Since(start), 500*time.Millisecond, "a pending import must not wait for hledger")
	for _, result := range []Result{first, second} {
		assert.True(t, result.Pending)
		assert.Empty(t, result.Err)
		assert.Empty(t, result.Transactions)
		assert.Equal(t, []string{data, data + ".rules"}, result.Sources, "sources are known before the conversion, for watching and protection")
	}

	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "OnReady was not called")
	}
	done := importer.Import(context.Background(), request)
	assert.False(t, done.Pending)
	require.Empty(t, done.Err)
	require.Len(t, done.Transactions, 1)
	assert.Equal(t, "1.5", done.Transactions[0].Postings[0].Amount.Quantity.String())
	assert.Equal(t, int32(1), calls.Load(), "two pending requests start one conversion")
}

// A failed conversion is a verdict too: it is cached and reported, not retried
// as pending forever.
func TestImport_BackgroundCachesAFailure(t *testing.T) {
	dir, data := fixture(t)
	binary := fakeHledger(t, dir, fakeRun{script: "echo 'hledger: Error: bad rules' >&2\nexit 1\n"})
	ready := make(chan struct{}, 1)
	importer := New(Config{Enabled: true, Path: binary, Timeout: 10 * time.Second, Background: true, OnReady: func() { ready <- struct{}{} }})
	request := Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20}

	require.True(t, importer.Import(context.Background(), request).Pending)
	<-ready
	result := importer.Import(context.Background(), request)
	assert.False(t, result.Pending)
	assert.Contains(t, result.Err, "hledger could not convert the file: bad rules")
}

// Without Background, as hledger-fmt uses it, Import waits for the conversion.
func TestImport_WithoutBackgroundWaits(t *testing.T) {
	dir, data := fixture(t)
	binary := slowValidHledger(t, dir, data, 300*time.Millisecond)
	importer := New(Config{Enabled: true, Path: binary, Timeout: 10 * time.Second})

	result := importer.Import(context.Background(), Request{Reader: ReaderCSV, Path: data, MaxFileSize: 1 << 20})

	assert.False(t, result.Pending)
	require.Empty(t, result.Err)
	assert.Len(t, result.Transactions, 1)
}
