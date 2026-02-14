package channels

import (
	"strings"
	"testing"
)

func TestMarkdownToTelegramHTML_PreservesDistinctInlineCodes(t *testing.T) {
	in := "Paths: `/Users/ernie/.picoclaw/workspace/IDENTITY.md`, `/Users/ernie/.picoclaw/workspace/AGENTS.md`, and `state/`."
	out := markdownToTelegramHTML(in)

	if !strings.Contains(out, "<code>/Users/ernie/.picoclaw/workspace/IDENTITY.md</code>") {
		t.Fatalf("missing first inline code, got: %s", out)
	}
	if !strings.Contains(out, "<code>/Users/ernie/.picoclaw/workspace/AGENTS.md</code>") {
		t.Fatalf("missing second inline code, got: %s", out)
	}
	if !strings.Contains(out, "<code>state/</code>") {
		t.Fatalf("missing third inline code, got: %s", out)
	}
}

func TestMarkdownToTelegramHTML_PreservesDistinctCodeBlocks(t *testing.T) {
	in := "```txt\nalpha\n```\n\n```txt\nbeta\n```"
	out := markdownToTelegramHTML(in)

	if !strings.Contains(out, "<pre><code>alpha\n</code></pre>") {
		t.Fatalf("missing first code block, got: %s", out)
	}
	if !strings.Contains(out, "<pre><code>beta\n</code></pre>") {
		t.Fatalf("missing second code block, got: %s", out)
	}
}
