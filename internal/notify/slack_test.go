package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/alvindcastro/blockscout/internal/storage"
)

// sampleLead is a realistic lead fixture used across multiple tests.
var sampleLead = storage.Lead{
	Source:                  "richmond_permits",
	Title:                   "Warehouse — 12500 Vulcan Way",
	Location:                "12500 Vulcan Way, Richmond BC",
	ProjectValue:            1_200_000,
	GeneralContractor:       "BuildRight Contracting",
	ProjectType:             "industrial",
	EstimatedCrewSize:       80,
	EstimatedDurationMonths: 6,
	OutOfTownCrewLikely:     true,
	PriorityScore:           9,
	PriorityReason:          "Large new industrial build near YVR — likely out-of-province steel crew",
	SuggestedOutreachTiming: "Reach out now — crews mobilizing in 4–6 weeks",
	Notes:                   "GC is BuildRight Contracting. Check LinkedIn for travel coordinator.",
	Status:                  "new",
	CreatedAt:               time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC),
}

// ── scoreEmoji ────────────────────────────────────────────────────────────────

func TestScoreEmoji(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{10, "🔥"},
		{9, "🔥"},
		{8, "⚡"},
		{7, "⚡"},
		{6, "👀"},
		{5, "👀"},
		{4, "📌"},
		{1, "📌"},
		{0, "📌"},
	}
	for _, tt := range tests {
		got := scoreEmoji(tt.score)
		if got != tt.want {
			t.Errorf("scoreEmoji(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

// ── formatCAD ─────────────────────────────────────────────────────────────────

func TestFormatCAD(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1_000, "1,000"},
		{500_000, "500,000"},
		{1_200_000, "1,200,000"},
		{57_290_092, "57,290,092"},
	}
	for _, tt := range tests {
		got := formatCAD(tt.input)
		if got != tt.want {
			t.Errorf("formatCAD(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── boolLabel ─────────────────────────────────────────────────────────────────

func TestBoolLabel(t *testing.T) {
	if got := boolLabel(true); got != "yes" {
		t.Errorf("boolLabel(true) = %q, want %q", got, "yes")
	}
	if got := boolLabel(false); got != "no" {
		t.Errorf("boolLabel(false) = %q, want %q", got, "no")
	}
}

// ── buildMessage ──────────────────────────────────────────────────────────────

func TestBuildMessage_structure(t *testing.T) {
	msg := buildMessage([]storage.Lead{sampleLead})

	blocks, ok := msg["blocks"].([]map[string]any)
	if !ok {
		t.Fatal("buildMessage: missing or wrong type for 'blocks'")
	}

	// Minimum: header + divider + section + divider = 4 blocks
	if len(blocks) < 4 {
		t.Fatalf("expected at least 4 blocks, got %d", len(blocks))
	}

	// First block must be header
	if typ, _ := blocks[0]["type"].(string); typ != "header" {
		t.Errorf("blocks[0] type = %q, want %q", typ, "header")
	}

	// Second block must be divider
	if typ, _ := blocks[1]["type"].(string); typ != "divider" {
		t.Errorf("blocks[1] type = %q, want %q", typ, "divider")
	}

	// Third block must be section (the lead)
	if typ, _ := blocks[2]["type"].(string); typ != "section" {
		t.Errorf("blocks[2] type = %q, want %q", typ, "section")
	}
}

func TestBuildMessage_multipleLeads(t *testing.T) {
	leads := []storage.Lead{sampleLead, sampleLead, sampleLead}
	msg := buildMessage(leads)

	blocks := msg["blocks"].([]map[string]any)
	// header + divider + (section + divider) * 3 = 2 + 6 = 8
	want := 8
	if len(blocks) != want {
		t.Errorf("3 leads: expected %d blocks, got %d", want, len(blocks))
	}
}

func TestBuildMessage_empty(t *testing.T) {
	// Send() returns early for empty leads, but buildMessage itself should
	// still produce a valid (header-only) structure if ever called directly.
	msg := buildMessage([]storage.Lead{})
	if _, ok := msg["blocks"]; !ok {
		t.Error("buildMessage: missing 'blocks' key")
	}
}

// ── leadBlock ─────────────────────────────────────────────────────────────────

func TestLeadBlock_containsTitle(t *testing.T) {
	block := leadBlock(sampleLead)

	text, ok := block["text"].(map[string]any)
	if !ok {
		t.Fatal("leadBlock: missing 'text' field")
	}
	content, _ := text["text"].(string)
	if !strings.Contains(content, sampleLead.Title) {
		t.Errorf("leadBlock text does not contain title %q\ngot: %s", sampleLead.Title, content)
	}
}

func TestLeadBlock_containsScore(t *testing.T) {
	block := leadBlock(sampleLead)

	text := block["text"].(map[string]any)
	content, _ := text["text"].(string)
	if !strings.Contains(content, "9/10") {
		t.Errorf("leadBlock text does not contain score '9/10'\ngot: %s", content)
	}
}

func TestLeadBlock_hasFields(t *testing.T) {
	block := leadBlock(sampleLead)

	fields, ok := block["fields"].([]map[string]any)
	if !ok {
		t.Fatal("leadBlock: missing 'fields'")
	}
	if len(fields) != 4 {
		t.Errorf("expected 4 fields, got %d", len(fields))
	}
}

// ── headerBlock ───────────────────────────────────────────────────────────────

func TestHeaderBlock_singular(t *testing.T) {
	block := headerBlock(1)
	text := block["text"].(map[string]any)
	s, _ := text["text"].(string)
	if !strings.Contains(s, "1 new lead") {
		t.Errorf("expected singular 'lead', got: %s", s)
	}
}

func TestHeaderBlock_plural(t *testing.T) {
	block := headerBlock(3)
	text := block["text"].(map[string]any)
	s, _ := text["text"].(string)
	if !strings.Contains(s, "3 new leads") {
		t.Errorf("expected plural 'leads', got: %s", s)
	}
}
