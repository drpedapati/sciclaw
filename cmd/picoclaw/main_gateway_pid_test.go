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
	orig := processCommandLineForPID
	t.Cleanup(func() { processCommandLineForPID = orig })

	t.Run("verified gateway process", func(t *testing.T) {
		processCommandLineForPID = func(pid int) (string, error) {
			if pid != 1234 {
				t.Fatalf("pid = %d, want 1234", pid)
			}
			return "/opt/homebrew/bin/sciclaw gateway", nil
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
		processCommandLineForPID = func(pid int) (string, error) {
			return "/usr/bin/login -fp ernie", nil
		}
		ok, err := isGatewayProcessPID(2222)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("expected non-gateway process to be rejected")
		}
	})

	t.Run("verification error bubbles up", func(t *testing.T) {
		processCommandLineForPID = func(pid int) (string, error) {
			return "", errors.New("ps failed")
		}
		ok, err := isGatewayProcessPID(3333)
		if err == nil {
			t.Fatal("expected verification error")
		}
		if ok {
			t.Fatal("expected false when verification fails")
		}
	})
}

