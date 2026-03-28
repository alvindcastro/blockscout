package collector

import (
	"context"
	"time"
)

// RawProject is the normalized output from any data source before enrichment.
// Every Collector returns a slice of these.
type RawProject struct {
	Source      string         // e.g. "richmond_permits", "bcbid", "news_rss"
	ExternalID  string         // unique ID from the source system
	Title       string         // project name or headline
	Location    string         // address or area
	Value       int64          // project value in CAD (dollars)
	Description string         // raw text description
	IssuedAt    time.Time      // date from the source (permit issue date, award date, etc.)
	SourceURL   string         // direct URL to the source document (PDF, page, feed item)
	RawData     map[string]any // full original payload preserved for audit
	Hash        string         // sha256 dedup key — set before inserting to DB
}

// Collector is the interface every data source must implement.
// Adding a new source means adding a new struct that satisfies this interface —
// the core pipeline does not change.
type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]RawProject, error)
}
