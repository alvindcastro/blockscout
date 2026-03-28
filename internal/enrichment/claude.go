package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alvindcastro/blockscout/internal/collector"
)

const (
	claudeAPIURL     = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion = "2023-06-01"
	defaultModel     = "claude-haiku-4-5-20251001"
)

// EnrichedLead is the structured output from the Claude enrichment step.
// Field names match the JSON keys Claude is prompted to return.
type EnrichedLead struct {
	GeneralContractor       string `json:"general_contractor"`
	ProjectType             string `json:"project_type"`
	EstimatedCrewSize       int    `json:"estimated_crew_size"`
	EstimatedDurationMonths int    `json:"estimated_duration_months"`
	OutOfTownCrewLikely     bool   `json:"out_of_town_crew_likely"`
	PriorityScore           int    `json:"priority_score"`
	PriorityReason          string `json:"priority_reason"`
	SuggestedOutreachTiming string `json:"suggested_outreach_timing"`
	Notes                   string `json:"notes"`
}

// ClaudeEnricher calls the Claude Messages API to enrich a RawProject into an EnrichedLead.
type ClaudeEnricher struct {
	APIKey string
	Model  string // defaults to claude-haiku-4-5-20251001; swap to claude-sonnet-4-6 if quality needs improvement
	client *http.Client
}

// NewClaudeEnricher returns a ClaudeEnricher using Haiku by default.
func NewClaudeEnricher(apiKey string) *ClaudeEnricher {
	return &ClaudeEnricher{
		APIKey: apiKey,
		Model:  defaultModel,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Enrich sends a RawProject to the Claude API and returns the parsed EnrichedLead.
func (c *ClaudeEnricher) Enrich(ctx context.Context, p collector.RawProject) (*EnrichedLead, error) {
	body, err := json.Marshal(c.buildRequest(p))
	if err != nil {
		return nil, fmt.Errorf("enrichment: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("enrichment: build request: %w", err)
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enrichment: api call: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("enrichment: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enrichment: claude returned HTTP %d: %s", resp.StatusCode, raw)
	}

	text, err := extractText(raw)
	if err != nil {
		return nil, err
	}

	var lead EnrichedLead
	if err := json.Unmarshal([]byte(stripMarkdown(text)), &lead); err != nil {
		return nil, fmt.Errorf("enrichment: parse claude json: %w\nraw response: %s", err, text)
	}

	return &lead, nil
}

// buildRequest assembles the Messages API payload.
func (c *ClaudeEnricher) buildRequest(p collector.RawProject) map[string]any {
	return map[string]any{
		"model":      c.Model,
		"max_tokens": 512,
		"system":     systemPrompt,
		"messages": []map[string]any{
			{"role": "user", "content": permitPrompt(p)},
		},
	}
}

// extractText pulls the assistant's text block from a Claude API response.
func extractText(raw []byte) (string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("enrichment: parse api response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("enrichment: claude error: %s", resp.Error.Message)
	}
	for _, block := range resp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("enrichment: no text block in claude response")
}

// stripMarkdown removes ```json ... ``` fences if Claude includes them despite instructions.
func stripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if lines := strings.SplitN(s, "\n", 2); len(lines) > 1 {
			s = lines[1]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

const systemPrompt = `You are a lead analyst for the Sandman Hotel Vancouver Airport in Richmond, BC (near YVR).
You evaluate building permit records to identify projects that will generate demand for construction crew lodging.

Key factors to weigh:
- Project value and type: new builds and major industrial/commercial projects bring large out-of-town crews
- Location: projects within 30 km of Richmond BC are the target area
- Contractor identity: larger or out-of-province GCs typically bring travelling crews
- Duration: longer projects mean extended-stay demand (room blocks, direct billing, weekly rates)

Respond with ONLY a valid JSON object. No markdown, no explanation, no code fences.`

// permitPrompt formats a RawProject as the user turn sent to Claude.
func permitPrompt(p collector.RawProject) string {
	return fmt.Sprintf(`Evaluate this building permit and return a JSON object with exactly these fields:
{
  "general_contractor": "company name, or \"unknown\"",
  "project_type": "one of: civil, commercial, industrial, utility, residential, unknown",
  "estimated_crew_size": <integer, 0 if unknown>,
  "estimated_duration_months": <integer, 0 if unknown>,
  "out_of_town_crew_likely": <true or false>,
  "priority_score": <integer 1-10, 10 = highest priority for hotel sales outreach>,
  "priority_reason": "one sentence explaining the score",
  "suggested_outreach_timing": "when and how the hotel should reach out",
  "notes": "any other details useful to the hotel sales team"
}

Permit data:
Source:   %s
Title:    %s
Location: %s
Value:    $%d CAD
Details:  %s
Issued:   %s`,
		p.Source, p.Title, p.Location, p.Value, p.Description, p.IssuedAt.Format("2006-01-02"),
	)
}
