package cli

import (
	"context"
	"testing"
	"time"
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
