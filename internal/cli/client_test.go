package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		timeout time.Duration
	}{
		{
			name:    "default path",
			path:    "hledger",
			timeout: 5 * time.Second,
		},
		{
			name:    "custom path",
			path:    "/usr/local/bin/hledger",
			timeout: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.path, tt.timeout)

			if client == nil {
				t.Fatal("expected non-nil client")
			}
			if client.path != tt.path {
				t.Errorf("path = %q; want %q", client.path, tt.path)
			}
			if client.timeout != tt.timeout {
				t.Errorf("timeout = %v; want %v", client.timeout, tt.timeout)
			}
		})
	}
}

func TestClient_Available(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantAvail bool
	}{
		{
			name:      "hledger in PATH",
			path:      "hledger",
			wantAvail: true,
		},
		{
			name:      "non-existent binary",
			path:      "/nonexistent/path/to/hledger",
			wantAvail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.path, 5*time.Second)
			got := client.Available()

			if tt.name == "hledger in PATH" {
				t.Logf("hledger available: %v (skip if not installed)", got)
				return
			}

			if got != tt.wantAvail {
				t.Errorf("Available() = %v; want %v", got, tt.wantAvail)
			}
		})
	}
}

func TestClient_Run(t *testing.T) {
	client := NewClient("hledger", 5*time.Second)
	if !client.Available() {
		t.Skip("hledger not available")
	}

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "version command",
			args:    []string{"--version"},
			wantErr: false,
		},
		{
			name:    "invalid command",
			args:    []string{"nonexistent-command-xyz"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := client.Run(ctx, "", tt.args...)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if output == "" {
					t.Error("expected non-empty output")
				}
			}
		})
	}
}

func TestClient_Run_WithFile(t *testing.T) {
	client := NewClient("hledger", 5*time.Second)
	if !client.Available() {
		t.Skip("hledger not available")
	}

	ctx := context.Background()
	output, err := client.Run(ctx, "/nonexistent/file.journal", "accounts")

	if err == nil {
		t.Error("expected error for non-existent file")
	}
	t.Logf("output: %s, err: %v", output, err)
}

func TestClient_Run_Timeout(t *testing.T) {
	client := NewClient("hledger", 1*time.Millisecond)
	if !client.Available() {
		t.Skip("hledger not available")
	}

	ctx := context.Background()
	_, err := client.Run(ctx, "", "--version")

	t.Logf("timeout test result: err=%v (may or may not timeout depending on system)", err)
}

func TestClient_Run_ContextCancellation(t *testing.T) {
	client := NewClient("hledger", 30*time.Second)
	if !client.Available() {
		t.Skip("hledger not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Run(ctx, "", "--version")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestClient_AvailabilityProbeIsLazyAndTimeBoxed(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow-hledger")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o700))

	start := time.Now()
	client := NewClient(script, 100*time.Millisecond)
	constructed := time.Since(start)

	assert.Less(t, constructed, time.Second,
		"constructing a client must not wait for the binary: the server does this while handling initialize")

	probeStart := time.Now()
	available := client.Available()
	probe := time.Since(probeStart)

	assert.False(t, available, "a binary that never answers is not available")
	assert.Less(t, probe, 3*time.Second, "the probe must be bounded by the configured timeout")

	// The probe is remembered, so a second call is free.
	secondStart := time.Now()
	assert.False(t, client.Available())
	assert.Less(t, time.Since(secondStart), 100*time.Millisecond, "the probe result is cached")
}

// fakeBinary writes a shell script standing in for hledger. It is used only to
// inject faults the real binary cannot be made to produce on demand.
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hledger")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700))
	return path
}

func TestClient_VersionIsParsedFromTheProbe(t *testing.T) {
	client := NewClient(fakeBinary(t, "echo 'hledger 1.99.5, linux-x86_64'\n"), 5*time.Second)
	require.True(t, client.Available())
	assert.Equal(t, "1.99.5", client.Version())

	other := NewClient(fakeBinary(t, "echo 'not hledger'\n"), 5*time.Second)
	require.True(t, other.Available())
	assert.Equal(t, "", other.Version(), "an unrecognised banner has no version")

	missing := NewClient("/nonexistent/hledger", 5*time.Second)
	assert.Equal(t, "", missing.Version())
}

func TestClient_RunBounded(t *testing.T) {
	versionLine := "if [ \"$1\" = --version ]; then echo 'hledger 1.99.5'; exit 0; fi\n"
	run := BoundedRun{Args: []string{"print"}, MaxStdout: 16, MaxStderr: 64}

	t.Run("returns stdout of a successful run", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"printf 'ok'\n"), 5*time.Second)
		out, err := client.RunBounded(context.Background(), run)
		require.NoError(t, err)
		assert.Equal(t, "ok", string(out))
	})

	t.Run("passes arguments without a shell", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"printf '%s|' \"$@\"\n"), 5*time.Second)
		out, err := client.RunBounded(context.Background(), BoundedRun{Args: []string{"a b", "$(x)", ";"}, MaxStdout: 64, MaxStderr: 64})
		require.NoError(t, err)
		assert.Equal(t, "a b|$(x)|;|", string(out))
	})

	t.Run("nonzero exit carries stderr", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"echo 'bad rules' >&2\nexit 1\n"), 5*time.Second)
		_, err := client.RunBounded(context.Background(), run)
		var exitErr *ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, "bad rules", exitErr.Stderr)
	})

	t.Run("stdout past the limit kills the run", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"while :; do echo 0123456789; done\n"), 5*time.Second)
		start := time.Now()
		_, err := client.RunBounded(context.Background(), run)
		require.ErrorIs(t, err, ErrOutputLimit)
		assert.Less(t, time.Since(start), 4*time.Second)
	})

	t.Run("stderr past the limit kills the run", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"while :; do echo 0123456789 >&2; done\n"), 5*time.Second)
		_, err := client.RunBounded(context.Background(), run)
		require.ErrorIs(t, err, ErrOutputLimit)
	})

	t.Run("timeout kills the run", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"exec sleep 30\n"), 300*time.Millisecond)
		require.True(t, client.Available())
		start := time.Now()
		_, err := client.RunBounded(context.Background(), run)
		require.ErrorIs(t, err, ErrTimeout)
		assert.Less(t, time.Since(start), 4*time.Second)
	})

	t.Run("cancellation kills the run", func(t *testing.T) {
		client := NewClient(fakeBinary(t, versionLine+"exec sleep 30\n"), 30*time.Second)
		require.True(t, client.Available())
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(200*time.Millisecond, cancel)
		start := time.Now()
		_, err := client.RunBounded(ctx, run)
		require.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(start), 4*time.Second)
	})

	t.Run("an unavailable binary is refused before running", func(t *testing.T) {
		client := NewClient("/nonexistent/hledger", 5*time.Second)
		_, err := client.RunBounded(context.Background(), run)
		require.ErrorIs(t, err, ErrUnavailable)
	})
}
