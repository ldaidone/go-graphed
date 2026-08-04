package graphed

import (
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestPayloadHash(t *testing.T) {
	a := payloadHash("same text")
	b := payloadHash("same text")
	c := payloadHash("different text")
	if a != b {
		t.Error("payloadHash is not deterministic")
	}
	if a == c {
		t.Error("payloadHash should differ for different payloads")
	}
	if len(a) != 64 {
		t.Errorf("payloadHash length = %d, want 64 (sha256 hex)", len(a))
	}
}

func TestTruncateBytes(t *testing.T) {
	if got := truncateBytes("short", 10); got != "short" {
		t.Errorf("truncateBytes short = %q, want %q", got, "short")
	}
	if got := truncateBytes("abcdefghij", 5); got != "abcde" {
		t.Errorf("truncateBytes long = %q, want %q", got, "abcde")
	}
}

func TestEntitySnippet(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}

	base := ir.Entity{Metadata: map[string]string{}}
	if got := entitySnippet(lines, base); got != "" {
		t.Errorf("entity without start_line should return empty, got %q", got)
	}

	e := ir.Entity{Metadata: map[string]string{"start_line": "2", "end_line": "4"}}
	if got := entitySnippet(lines, e); got != "b\nc\nd" {
		t.Errorf("entitySnippet = %q, want %q", got, "b\nc\nd")
	}

	// Out-of-range start is a no-op.
	e = ir.Entity{Metadata: map[string]string{"start_line": "99"}}
	if got := entitySnippet(lines, e); got != "" {
		t.Errorf("entitySnippet out of range = %q, want empty", got)
	}

	// Malformed line metadata is a no-op.
	e = ir.Entity{Metadata: map[string]string{"start_line": "x"}}
	if got := entitySnippet(lines, e); got != "" {
		t.Errorf("entitySnippet malformed = %q, want empty", got)
	}

	// Snippet is bounded by entitySnippetMax.
	long := strings.Repeat("x", entitySnippetMax*2)
	lines2 := []string{long}
	e = ir.Entity{Metadata: map[string]string{"start_line": "1", "end_line": "1"}}
	if got := entitySnippet(lines2, e); len(got) > entitySnippetMax {
		t.Errorf("entitySnippet length = %d, want <= %d", len(got), entitySnippetMax)
	}
}

func TestEmbedWorkers(t *testing.T) {
	if got := embedWorkers(0); got < 1 || got > embedWorkersMax {
		t.Errorf("embedWorkers(0) = %d, want 1..%d", got, embedWorkersMax)
	}
	if got := embedWorkers(4); got != 4 {
		t.Errorf("embedWorkers(4) = %d, want 4", got)
	}
	if got := embedWorkers(64); got != embedWorkersMax {
		t.Errorf("embedWorkers(64) = %d, want %d", got, embedWorkersMax)
	}
}
