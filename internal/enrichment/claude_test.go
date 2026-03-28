package enrichment

import (
	"strings"
	"testing"
)

// ── stripMarkdown ─────────────────────────────────────────────────────────────

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain json — unchanged", `{"key": "val"}`, `{"key": "val"}`},
		{"fenced json block", "```json\n{\"key\": \"val\"}\n```", `{"key": "val"}`},
		{"fenced no language tag", "```\n{\"key\": \"val\"}\n```", `{"key": "val"}`},
		{"leading/trailing whitespace", "  {\"key\": \"val\"}  ", `{"key": "val"}`},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		got := stripMarkdown(tt.input)
		if got != tt.want {
			t.Errorf("[%s] stripMarkdown(%q) = %q, want %q", tt.name, tt.input, got, tt.want)
		}
	}
}

// ── extractText ───────────────────────────────────────────────────────────────

func TestExtractText_returnsFirstTextBlock(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"{\"priority_score\":9}"}]}`)
	got, err := extractText(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"priority_score":9}` {
		t.Errorf("got %q, want JSON string", got)
	}
}

func TestExtractText_skipsNonTextBlocks(t *testing.T) {
	// tool_use block before the text block — should skip it and return text
	raw := []byte(`{"content":[{"type":"tool_use","id":"abc"},{"type":"text","text":"ok"}]}`)
	got, err := extractText(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}

func TestExtractText_errorOnAPIError(t *testing.T) {
	raw := []byte(`{"error":{"type":"invalid_request_error","message":"credit balance too low"}}`)
	_, err := extractText(raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "credit balance too low") {
		t.Errorf("error should contain API message, got: %v", err)
	}
}

func TestExtractText_errorWhenNoTextBlock(t *testing.T) {
	raw := []byte(`{"content":[{"type":"tool_use","id":"abc"}]}`)
	_, err := extractText(raw)
	if err == nil {
		t.Fatal("expected error when no text block, got nil")
	}
}

func TestExtractText_errorOnMalformedJSON(t *testing.T) {
	_, err := extractText([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
}
