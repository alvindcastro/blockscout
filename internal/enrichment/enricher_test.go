package enrichment

import (
	"testing"
	"time"

	"github.com/alvindcastro/blockscout/internal/collector"
)

// ── toLeadRecord ──────────────────────────────────────────────────────────────

func TestToLeadRecord_fieldMapping(t *testing.T) {
	issued := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	p := collector.RawProject{
		Source:   "richmond_permits",
		Title:    "Hotel — 8640 Alexandra Road",
		Location: "8640 Alexandra Road",
		Value:    300_000,
		IssuedAt: issued,
		RawData: map[string]any{
			"applicant":  "Studio Senbel Architecture Inc (604)605-6995",
			"contractor": "Safara Cladding Inc (416)875-1770",
		},
	}
	e := &EnrichedLead{
		GeneralContractor:       "Safara Cladding Inc",
		ProjectType:             "commercial",
		EstimatedCrewSize:       15,
		EstimatedDurationMonths: 2,
		OutOfTownCrewLikely:     false,
		PriorityScore:           4,
		PriorityReason:          "Small alteration — local crew likely",
		SuggestedOutreachTiming: "Low priority — monitor for future phases",
		Notes:                   "Cladding alteration only.",
	}

	lead := toLeadRecord(p, e)

	// RawProject fields
	if lead.Source != "richmond_permits" {
		t.Errorf("Source = %q, want %q", lead.Source, "richmond_permits")
	}
	if lead.Title != p.Title {
		t.Errorf("Title = %q, want %q", lead.Title, p.Title)
	}
	if lead.Location != p.Location {
		t.Errorf("Location = %q, want %q", lead.Location, p.Location)
	}
	if lead.ProjectValue != p.Value {
		t.Errorf("ProjectValue = %d, want %d", lead.ProjectValue, p.Value)
	}

	// Raw applicant/contractor from RawData (phone numbers preserved)
	if lead.Applicant != "Studio Senbel Architecture Inc (604)605-6995" {
		t.Errorf("Applicant = %q, want phone number in string", lead.Applicant)
	}
	if lead.Contractor != "Safara Cladding Inc (416)875-1770" {
		t.Errorf("Contractor = %q, want phone number in string", lead.Contractor)
	}

	// EnrichedLead fields
	if lead.GeneralContractor != e.GeneralContractor {
		t.Errorf("GeneralContractor = %q, want %q", lead.GeneralContractor, e.GeneralContractor)
	}
	if lead.PriorityScore != 4 {
		t.Errorf("PriorityScore = %d, want 4", lead.PriorityScore)
	}
	if lead.OutOfTownCrewLikely != false {
		t.Errorf("OutOfTownCrewLikely = %v, want false", lead.OutOfTownCrewLikely)
	}
}

func TestToLeadRecord_missingRawDataKeys(t *testing.T) {
	p := collector.RawProject{
		Source:  "richmond_permits",
		RawData: map[string]any{}, // no applicant or contractor keys
	}
	lead := toLeadRecord(p, &EnrichedLead{})
	if lead.Applicant != "" {
		t.Errorf("Applicant should be empty when key absent, got %q", lead.Applicant)
	}
	if lead.Contractor != "" {
		t.Errorf("Contractor should be empty when key absent, got %q", lead.Contractor)
	}
}

func TestToLeadRecord_nilRawData(t *testing.T) {
	p := collector.RawProject{
		Source:  "richmond_permits",
		RawData: nil,
	}
	// Should not panic when RawData is nil
	lead := toLeadRecord(p, &EnrichedLead{})
	if lead.Applicant != "" || lead.Contractor != "" {
		t.Errorf("expected empty strings for nil RawData, got applicant=%q contractor=%q",
			lead.Applicant, lead.Contractor)
	}
}
