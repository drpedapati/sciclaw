package channels

import (
	"strings"
	"testing"
)

func TestSplitTelegramMessage_RespectsMaxRunes(t *testing.T) {
	input := strings.Repeat("a", 10050)
	chunks := splitTelegramMessage(input, 3900)

	if len(chunks) < 3 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	total := 0
	for i, chunk := range chunks {
		if runeCount(chunk) > 3900 {
			t.Fatalf("chunk %d exceeds max runes: %d", i, runeCount(chunk))
		}
		total += runeCount(chunk)
	}

	if total != len(input) {
		t.Fatalf("chunked rune total mismatch: got %d want %d", total, len(input))
	}
}

func TestSplitTelegramMessage_PrefersNewlineBoundaries(t *testing.T) {
	line := strings.Repeat("x", 80)
	input := strings.Join([]string{line, line, line, line, line}, "\n")

	chunks := splitTelegramMessage(input, 170)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		for _, part := range strings.Split(chunk, "\n") {
			if part == "" {
				continue
			}
			if len(part) != 80 {
				t.Fatalf("chunk %d split in middle of line, segment length=%d", i, len(part))
			}
		}
	}
}

func TestSplitTelegramMessage_EmptyInput(t *testing.T) {
	chunks := splitTelegramMessage("   \n\t", 100)
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for empty input, got %d", len(chunks))
	}
}
