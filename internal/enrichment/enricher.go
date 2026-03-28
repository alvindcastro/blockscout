package enrichment

import (
	"context"
	"fmt"
	"log"

	"github.com/alvindcastro/blockscout/internal/collector"
	"github.com/alvindcastro/blockscout/internal/storage"
)

// Enricher orchestrates the collect → dedup → enrich → store pipeline.
// It runs every registered Collector, skips permits already in the DB,
// calls Claude to enrich new ones, and writes the resulting Lead records.
type Enricher struct {
	collectors []collector.Collector
	rawStore   storage.RawProjectStore
	leadStore  storage.LeadStore
	claude     *ClaudeEnricher
	Verbose    bool
}

// NewEnricher wires together all pipeline dependencies.
func NewEnricher(
	collectors []collector.Collector,
	rawStore storage.RawProjectStore,
	leadStore storage.LeadStore,
	claude *ClaudeEnricher,
) *Enricher {
	return &Enricher{
		collectors: collectors,
		rawStore:   rawStore,
		leadStore:  leadStore,
		claude:     claude,
	}
}

// Run executes the full pipeline for every registered collector.
// It returns the number of new leads inserted.
// Collector-level and enrichment-level errors are logged and skipped so a
// single bad record does not abort the entire run.
func (e *Enricher) Run(ctx context.Context) (int, error) {
	var newLeads int

	for _, c := range e.collectors {
		n, err := e.runCollector(ctx, c)
		if err != nil {
			return newLeads, err
		}
		newLeads += n
	}

	return newLeads, nil
}

// runCollector processes all projects from a single Collector.
func (e *Enricher) runCollector(ctx context.Context, c collector.Collector) (int, error) {
	projects, err := c.Collect(ctx)
	if err != nil {
		// Log and skip — don't abort the whole run for one collector
		log.Printf("[enricher] %s: collect failed: %v", c.Name(), err)
		return 0, nil
	}

	if e.Verbose {
		log.Printf("[enricher] %s: %d projects collected", c.Name(), len(projects))
	}

	var newLeads int
	for _, p := range projects {
		inserted, err := e.processProject(ctx, p)
		if err != nil {
			return newLeads, fmt.Errorf("enricher: %s: %w", c.Name(), err)
		}
		if inserted {
			newLeads++
		}
	}

	if e.Verbose {
		log.Printf("[enricher] %s: %d new leads inserted", c.Name(), newLeads)
	}
	return newLeads, nil
}

// processProject deduplicates, enriches, and stores a single RawProject.
// Returns true if a new lead was inserted.
func (e *Enricher) processProject(ctx context.Context, p collector.RawProject) (bool, error) {
	// Dedup check — skip if we've seen this permit before
	exists, err := e.rawStore.ExistsByHash(ctx, p.Hash)
	if err != nil {
		return false, fmt.Errorf("check hash: %w", err)
	}
	if exists {
		if e.Verbose {
			log.Printf("[enricher] skip duplicate: %s", p.ExternalID)
		}
		return false, nil
	}

	// Persist the raw record before enrichment so it's never lost
	if err := e.rawStore.Insert(ctx, &p); err != nil {
		return false, fmt.Errorf("insert raw project: %w", err)
	}

	// Enrich via Claude
	enriched, err := e.claude.Enrich(ctx, p)
	if err != nil {
		// Log and skip — don't fail the run over a single API error
		log.Printf("[enricher] enrich failed for %q: %v", p.Title, err)
		return false, nil
	}

	// Persist the lead
	lead := toLeadRecord(p, enriched)
	if err := e.leadStore.Insert(ctx, &lead); err != nil {
		return false, fmt.Errorf("insert lead: %w", err)
	}

	if e.Verbose {
		log.Printf("[enricher] new lead: %q  score=%d  gc=%q",
			p.Title, enriched.PriorityScore, enriched.GeneralContractor)
	}
	return true, nil
}

// toLeadRecord maps a RawProject + EnrichedLead into a storage.Lead.
// Applicant and Contractor are taken directly from the raw permit data so that
// phone numbers and contact details from the PDF are preserved as-is.
func toLeadRecord(p collector.RawProject, e *EnrichedLead) storage.Lead {
	applicant, _ := p.RawData["applicant"].(string)
	contractor, _ := p.RawData["contractor"].(string)
	return storage.Lead{
		Source:                  p.Source,
		Title:                   p.Title,
		Location:                p.Location,
		ProjectValue:            p.Value,
		GeneralContractor:       e.GeneralContractor,
		Applicant:               applicant,
		Contractor:              contractor,
		SourceURL:               p.SourceURL,
		ProjectType:             e.ProjectType,
		EstimatedCrewSize:       e.EstimatedCrewSize,
		EstimatedDurationMonths: e.EstimatedDurationMonths,
		OutOfTownCrewLikely:     e.OutOfTownCrewLikely,
		PriorityScore:           e.PriorityScore,
		PriorityReason:          e.PriorityReason,
		SuggestedOutreachTiming: e.SuggestedOutreachTiming,
		Notes:                   e.Notes,
	}
}
