package main

import "testing"

func TestLookPathWithFallbackFindsBinaryOutsidePATH(t *testing.T) {
	t.Setenv("PATH", "/tmp/definitely-not-a-real-bin")

	p, err := lookPathWithFallback("sh")
	if err != nil {
		t.Fatalf("lookPathWithFallback(sh) returned error: %v", err)
	}
	if p == "" {
		t.Fatalf("lookPathWithFallback(sh) returned empty path")
	}
}

func TestLookPathWithFallbackReturnsErrorForMissingBinary(t *testing.T) {
	t.Setenv("PATH", "/tmp/definitely-not-a-real-bin")

	if _, err := lookPathWithFallback("sciclaw-definitely-missing-binary-name"); err == nil {
		t.Fatalf("expected error for missing binary")
	}
}

