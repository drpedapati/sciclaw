package main

import "testing"

func TestIsTinyTeXAutoInstallSupported(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   bool
	}{
		{name: "linux arm64 unsupported", goos: "linux", goarch: "arm64", want: false},
		{name: "linux arm unsupported", goos: "linux", goarch: "arm", want: false},
		{name: "linux amd64 supported", goos: "linux", goarch: "amd64", want: true},
		{name: "darwin arm64 supported", goos: "darwin", goarch: "arm64", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTinyTeXAutoInstallSupported(tt.goos, tt.goarch); got != tt.want {
				t.Fatalf("isTinyTeXAutoInstallSupported(%q, %q) = %v, want %v", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestIsTinyTeXUnsupportedOutput(t *testing.T) {
	out := "This platform doesn't support installation at this time. Please install manually instead."
	if !isTinyTeXUnsupportedOutput(out) {
		t.Fatalf("expected unsupported TinyTeX output to be detected")
	}
}

func TestSummarizeTinyTeXInstallOutputSkipsStackTraceNoise(t *testing.T) {
	out := `Installing tinytex
ERROR: network timeout while downloading package index
ERROR: [non-error-thrown] undefined

Stack trace:
    at _Command.handleError
    at mainRunner`

	got := summarizeTinyTeXInstallOutput(out)
	if got != "ERROR: network timeout while downloading package index" {
		t.Fatalf("unexpected summary: %q", got)
	}
}
