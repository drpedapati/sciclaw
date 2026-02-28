package main

import (
	"errors"
	"testing"
)

func TestIsGatewayProcessCommandLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "sciclaw gateway direct",
			in:   "/opt/homebrew/bin/sciclaw gateway",
			want: true,
		},
		{
			name: "picoclaw gateway with args",
			in:   "/usr/local/bin/picoclaw gateway --debug",
			want: true,
		},
		{
			name: "env wrapper still gateway",
			in:   "env HOME=/Users/tester /opt/homebrew/bin/sciclaw gateway",
			want: true,
		},
		{
			name: "sciclaw service status is not gateway",
			in:   "/opt/homebrew/bin/sciclaw service status",
			want: false,
		},
		{
			name: "sciclaw with gateway flag value is not gateway subcommand",
			in:   "/opt/homebrew/bin/sciclaw --profile gateway",
			want: false,
		},
		{
			name: "unrelated process",
			in:   "/usr/bin/python3 worker.py",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGatewayProcessCommandLine(tt.in)
			if got != tt.want {
				t.Fatalf("isGatewayProcessCommandLine(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsGatewayProcessPID(t *testing.T) {
	orig := pgrepGatewayPIDs
	t.Cleanup(func() { pgrepGatewayPIDs = orig })

	t.Run("verified gateway process", func(t *testing.T) {
		pgrepGatewayPIDs = func() ([]byte, error) {
			return []byte("1234\n5678\n"), nil
		}
		ok, err := isGatewayProcessPID(1234)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected pid to be treated as gateway process")
		}
	})

	t.Run("reused pid for unrelated process", func(t *testing.T) {
		pgrepGatewayPIDs = func() ([]byte, error) {
			return []byte("5678\n9999\n"), nil
		}
		ok, err := isGatewayProcessPID(2222)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("expected non-gateway process to be rejected")
		}
	})

	t.Run("no gateway processes running", func(t *testing.T) {
		pgrepGatewayPIDs = func() ([]byte, error) {
			return nil, &fakeExitError{code: 1}
		}
		ok, err := isGatewayProcessPID(3333)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("expected false when no gateway processes match")
		}
	})

	t.Run("pgrep error bubbles up", func(t *testing.T) {
		pgrepGatewayPIDs = func() ([]byte, error) {
			return nil, errors.New("pgrep failed")
		}
		ok, err := isGatewayProcessPID(4444)
		if err == nil {
			t.Fatal("expected pgrep error")
		}
		if ok {
			t.Fatal("expected false when pgrep fails")
		}
	})
}

// fakeExitError simulates an exec.ExitError for testing pgrep exit codes.
type fakeExitError struct {
	code int
}

func (e *fakeExitError) Error() string { return "exit status" }
func (e *fakeExitError) ExitCode() int { return e.code }

