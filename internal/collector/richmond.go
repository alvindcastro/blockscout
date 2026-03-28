package collector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

const (
	richmondBaseURL    = "https://www.richmond.ca"
	richmondReportsURL = "https://www.richmond.ca/business-development/building-approvals/reports/weeklyreports.htm"
)

// buildingReportRe matches the href path of building report PDFs on the weekly reports page.
// Example match: /__shared/assets/buildingreportmarch15_202678649.pdf
var buildingReportRe = regexp.MustCompile(`/__shared/assets/buildingreport[^"]+\.pdf`)

// RichmondCollector scrapes building permit PDFs published weekly by the City of Richmond BC.
// Richmond has no open data API — data is only available as PDFs at:
// https://www.richmond.ca/business-development/building-approvals/reports/weeklyreports.htm
type RichmondCollector struct {
	client *http.Client
}

// NewRichmondCollector returns a RichmondCollector with a 30-second HTTP timeout.
func NewRichmondCollector() *RichmondCollector {
	return &RichmondCollector{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name satisfies the Collector interface.
func (r *RichmondCollector) Name() string { return "richmond_permits" }

// Collect satisfies the Collector interface.
// Full implementation added in A2 + A3. Returns empty slice until then.
func (r *RichmondCollector) Collect(ctx context.Context) ([]RawProject, error) {
	urls, err := r.fetchPDFURLs(ctx)
	if err != nil {
		return nil, err
	}
	// A2: download + parse each PDF
	// A3: filter + map to RawProject
	_ = urls
	return nil, nil
}

// fetchPDFURLs scrapes the weekly reports page and returns absolute URLs for all
// building report PDFs listed. Demolition report PDFs are excluded.
func (r *RichmondCollector) fetchPDFURLs(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, richmondReportsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("richmond: build request: %w", err)
	}
	// Identify the bot politely — Richmond may rate-limit anonymous scrapers.
	req.Header.Set("User-Agent", "blockscout-leadgen/1.0 (hotel group sales intelligence)")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("richmond: fetch reports page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("richmond: reports page returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("richmond: read response body: %w", err)
	}

	paths := buildingReportRe.FindAllString(string(body), -1)
	if len(paths) == 0 {
		return nil, fmt.Errorf("richmond: no building report PDFs found — page structure may have changed")
	}

	// Deduplicate (same PDF can appear multiple times in the HTML) and make absolute URLs.
	seen := make(map[string]bool)
	urls := make([]string, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			urls = append(urls, richmondBaseURL+p)
		}
	}
	return urls, nil
}
