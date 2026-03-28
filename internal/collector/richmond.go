package collector

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	richmondBaseURL    = "https://www.richmond.ca"
	richmondReportsURL = "https://www.richmond.ca/business-development/building-approvals/reports/weeklyreports.htm"
)

// buildingReportRe matches building report PDF href paths on the weekly reports page.
// Example: /__shared/assets/buildingreportmarch15_202678649.pdf
var buildingReportRe = regexp.MustCompile(`/__shared/assets/buildingreport[^"]+\.pdf`)

// folderNumRe identifies the start of a new permit record.
// Folder numbers follow the pattern: 25 036523 000 00 B7
var folderNumRe = regexp.MustCompile(`^\d{2}\s+\d{6}`)

// subTypeRe matches SUB TYPE section headers, e.g. "SUB TYPE: Hotel"
var subTypeRe = regexp.MustCompile(`(?i)SUB\s+TYPE:\s*(.+)`)

// skipLineRe matches lines that are column headers, totals, or page noise to ignore during parsing.
var skipLineRe = regexp.MustCompile(`(?i)^(folder\s*number|work\s*proposed|status|issue\s*date|constr|applicant|contractor|sub\s*total|grand\s*total|building\s*permit|city\s*of\s*richmond)`)

// issueDateRe matches YYYY/MM/DD date strings.
var issueDateRe = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}$`)

// valueRe matches dollar amounts like $300,000.00
var valueRe = regexp.MustCompile(`^\$[\d,]+\.?\d*$`)

// folderSuffixRe matches the trailing type code of a Richmond folder number (e.g. "B7").
// pdftotext sometimes wraps the folder number across two lines; this code appears alone on the second line.
var folderSuffixRe = regexp.MustCompile(`^[A-Z]\d+$`)

// folderNumExtractRe matches a complete folder number within a line.
// Used to split "19 878924 000 02 B7 Special Inspection Issued" into number + rest.
var folderNumExtractRe = regexp.MustCompile(`\d{2}\s+\d{6}\s+\d{3}\s+\d{2}\s+[A-Z]\d+`)

// folderNameHeaderRe matches "FOLDER NAME" as a standalone header line.
var folderNameHeaderRe = regexp.MustCompile(`(?i)^folder\s+name\s*$`)

// folderNameDataRe matches "FOLDER NAME 8640 Alexandra Road" — label + address on one line.
var folderNameDataRe = regexp.MustCompile(`(?i)^folder\s+name\s+(.+)`)

// permitCountRe matches a lone integer printed as a row count in the PDF (e.g. "1").
var permitCountRe = regexp.MustCompile(`^\d+$`)

// permitRecord holds the raw fields extracted from one permit entry in a Richmond PDF report.
// Fields are detected by content (date format, dollar sign, FOLDER NAME prefix, etc.)
// rather than position — the actual pdftotext output includes extra lines (permit count
// integers, column header repeats) that make positional parsing unreliable.
type permitRecord struct {
	SubType      string // e.g. "Hotel", "Warehouse", "Office"
	FolderNumber string // e.g. "25 036523 000 00 B7"
	WorkProposed string // e.g. "New", "Alteration", "Revision"
	Status       string // e.g. "Issued"
	IssueDate    time.Time
	ValueCAD     int64  // construction value in CAD dollars
	Address      string // civic address + project description
	Applicant    string
	Contractor   string
}

// RichmondCollector scrapes building permit PDFs published weekly by the City of Richmond BC.
// Richmond has no open data API — data is only available as PDFs at:
// https://www.richmond.ca/business-development/building-approvals/reports/weeklyreports.htm
type RichmondCollector struct {
	client   *http.Client
	Verbose  bool  // when true, logs intermediate step counts to stderr
	MinValue int64 // minimum construction value to pass the filter (default: minPermitValueCAD)
}

// NewRichmondCollector returns a RichmondCollector with a 30-second HTTP timeout.
func NewRichmondCollector() *RichmondCollector {
	return &RichmondCollector{
		client:   &http.Client{Timeout: 30 * time.Second},
		MinValue: minPermitValueCAD,
	}
}

// Name satisfies the Collector interface.
func (r *RichmondCollector) Name() string { return "richmond_permits" }

// Collect satisfies the Collector interface. Downloads the most recent weekly
// building report PDF, parses all permit records, and returns them as RawProjects.
// Filter + mapping logic added in A3.
func (r *RichmondCollector) Collect(ctx context.Context) ([]RawProject, error) {
	urls, err := r.fetchPDFURLs(ctx)
	if err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("richmond: no PDF URLs found")
	}

	// Always process only the most recent report (first link = latest week).
	if r.Verbose {
		log.Printf("[richmond] found %d PDF URLs, using: %s", len(urls), urls[0])
	}

	path, cleanup, err := r.downloadPDF(ctx, urls[0])
	if err != nil {
		return nil, err
	}
	defer cleanup()

	records, err := parsePDF(path)
	if err != nil {
		return nil, err
	}

	if r.Verbose {
		log.Printf("[richmond] parsed %d raw permit records from PDF", len(records))
		counts := make(map[string]int)
		for _, rec := range records {
			counts[rec.SubType]++
		}
		for subType, n := range counts {
			log.Printf("[richmond]   sub-type %-30q  %d permits", subType, n)
		}
	}

	pdfURL := urls[0]
	var projects []RawProject
	for _, rec := range records {
		if !isRelevant(rec, r.MinValue) {
			continue
		}
		p := toRawProject(rec)
		p.SourceURL = pdfURL
		p.Hash = hashPermit(rec.FolderNumber, rec.Address, rec.IssueDate)
		projects = append(projects, p)
	}

	if r.Verbose {
		log.Printf("[richmond] %d permits passed filter (commercial + value > $%s CAD)", len(projects), formatValue(r.MinValue))
	}

	return projects, nil
}

// fetchPDFURLs scrapes the weekly reports page and returns absolute URLs for
// building report PDFs. Demolition reports are excluded by the regex.
func (r *RichmondCollector) fetchPDFURLs(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, richmondReportsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("richmond: build request: %w", err)
	}
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

// downloadPDF fetches a PDF from url, writes it to a temp file, and returns
// the file path plus a cleanup function the caller must defer.
func (r *RichmondCollector) downloadPDF(ctx context.Context, url string) (path string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("richmond: build pdf request: %w", err)
	}
	req.Header.Set("User-Agent", "blockscout-leadgen/1.0 (hotel group sales intelligence)")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("richmond: download pdf: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("richmond: pdf download returned HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "richmond-*.pdf")
	if err != nil {
		return "", nil, fmt.Errorf("richmond: create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("richmond: write pdf: %w", err)
	}
	tmp.Close()

	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

// parsePDF extracts all permit records from a Richmond PDF report.
// It shells out to pdftotext (Poppler), which correctly handles Richmond's PDF font encoding.
// The plain-text output is split into lines and fed to parsePermitLines.
func parsePDF(path string) ([]permitRecord, error) {
	pdftotext, err := findPdftotext()
	if err != nil {
		return nil, err
	}

	out, err := exec.Command(pdftotext, path, "-").Output()
	if err != nil {
		return nil, fmt.Errorf("richmond: pdftotext: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}

	return parsePermitLines(lines), nil
}

// findPdftotext returns the path to the pdftotext binary.
// Checks the location bundled with Git for Windows first, then falls back to PATH.
func findPdftotext() (string, error) {
	const gitPath = `C:\Program Files\Git\mingw64\bin\pdftotext.exe`
	if _, err := os.Stat(gitPath); err == nil {
		return gitPath, nil
	}
	p, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", fmt.Errorf("richmond: pdftotext not found — install Poppler or Git for Windows (expected at %s)", gitPath)
	}
	return p, nil
}

// parsePermitLines converts a flat slice of text lines into permit records.
//
// Detection is content-aware rather than positional: each line is classified by
// what it looks like, so the extra lines pdftotext inserts (permit-count integers,
// "CONSTR. VALUE" headers, "FOLDER NAME …" address labels) do not displace real values.
//
// Detection order per line:
//  1. SUB TYPE header → flush current, update sub-type
//  2. skipLineRe match → discard (column headers, totals, page chrome)
//  3. folderNumRe match → flush current, start new record
//  4. folderSuffixRe (e.g. "B7") with no fields set → append to folder number
//  5. nextLineIsAddress flag set → this line is the address
//  6. folderNameDataRe ("FOLDER NAME 8640 …") → address from capture group
//  7. folderNameHeaderRe ("FOLDER NAME" alone) → set nextLineIsAddress flag
//  8. issueDateRe → IssueDate
//  9. valueRe → first match = ValueCAD; subsequent = subtotal, skip
//  10. permitCountRe (bare integer) → skip (permit-count row)
//  11. everything else → sequential fill: WorkProposed → Status → Address → Applicant → Contractor
//     (fields already set, e.g. Address via FOLDER NAME, are skipped automatically)
func parsePermitLines(lines []string) []permitRecord {
	var records []permitRecord
	var current *permitRecord
	var currentSubType string
	var nextLineIsAddress bool

	flush := func() {
		if current != nil {
			records = append(records, *current)
			current = nil
		}
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// 1. Section header — flush current record, update sub-type
		if m := subTypeRe.FindStringSubmatch(line); m != nil {
			flush()
			currentSubType = strings.TrimSpace(m[1])
			nextLineIsAddress = false
			continue
		}

		// 2. Skip column headers, totals, and page chrome
		if skipLineRe.MatchString(line) {
			continue
		}

		// 3. New permit record starts with a folder number
		if folderNumRe.MatchString(line) {
			flush()
			fn := line
			if m := folderNumExtractRe.FindString(line); m != "" {
				fn = m
			}
			current = &permitRecord{
				SubType:      currentSubType,
				FolderNumber: fn,
			}
			nextLineIsAddress = false
			continue
		}

		if current == nil {
			continue
		}

		// 4. pdftotext sometimes wraps the trailing type code (e.g. "B7") to its own line.
		// Accept only when no text fields have been assigned yet.
		if current.WorkProposed == "" && folderSuffixRe.MatchString(line) {
			current.FolderNumber = strings.TrimSpace(current.FolderNumber + " " + line)
			continue
		}

		// 5. Address continuation from "FOLDER NAME" header on previous line
		if nextLineIsAddress {
			current.Address = line
			nextLineIsAddress = false
			continue
		}

		// 6. "FOLDER NAME 8640 Alexandra Road" — address embedded on same line
		if m := folderNameDataRe.FindStringSubmatch(line); m != nil {
			current.Address = strings.TrimSpace(m[1])
			continue
		}

		// 7. "FOLDER NAME" alone — address is on the next line
		if folderNameHeaderRe.MatchString(line) {
			nextLineIsAddress = true
			continue
		}

		// 8. Date line
		if issueDateRe.MatchString(line) {
			if t, err := time.Parse("2006/01/02", line); err == nil {
				current.IssueDate = t
			}
			continue
		}

		// 9. Dollar value — first occurrence is construction value; subsequent are subtotals
		if valueRe.MatchString(line) {
			if current.ValueCAD == 0 {
				current.ValueCAD = parseDollarAmount(line)
			}
			continue
		}

		// 10. Lone integer — permit count row printed by pdftotext, skip
		if permitCountRe.MatchString(line) {
			continue
		}

		// 11. Sequential text assignment; already-set fields are skipped automatically
		switch {
		case current.WorkProposed == "":
			current.WorkProposed = line
		case current.Status == "":
			current.Status = line
		case current.Address == "":
			current.Address = line
		case current.Applicant == "":
			current.Applicant = line
		case current.Contractor == "":
			current.Contractor = line
		}
	}

	flush()
	return records
}

// parseDollarAmount converts "$300,000.00" → 300000.
func parseDollarAmount(s string) int64 {
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, ",", "")
	if dot := strings.Index(s, "."); dot != -1 {
		s = s[:dot]
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// minPermitValueCAD is the minimum construction value for a permit to be considered.
// Residential and low-value permits rarely involve out-of-town crews.
const minPermitValueCAD = 500_000

// commercialSubTypes is the whitelist of permit sub-types relevant to hotel group sales.
// Residential sub-types (One Family Dwelling, Townhouse, etc.) are excluded — they
// don't generate construction crew lodging demand at scale.
var commercialSubTypes = map[string]bool{
	"hotel":                true,
	"warehouse":            true,
	"office":               true,
	"medical office":       true,
	"dental office":        true,
	"restaurant":           true,
	"retail":               true,
	"apartment":            true,
	"educational facility": true,
	"community hall":       true,
	"recreational":         true,
	"industrial":           true,
	"canopy":               true,
}

// isRelevant returns true if a permit record is worth enriching.
// Filters out residential sub-types and permits at or below minValue.
func isRelevant(rec permitRecord, minValue int64) bool {
	if rec.ValueCAD <= minValue {
		return false
	}
	return commercialSubTypes[strings.ToLower(strings.TrimSpace(rec.SubType))]
}

// toRawProject maps a permitRecord to the normalized RawProject used by the pipeline.
func toRawProject(rec permitRecord) RawProject {
	return RawProject{
		Source:     "richmond_permits",
		ExternalID: rec.FolderNumber,
		Title:      fmt.Sprintf("%s — %s", rec.SubType, rec.Address),
		Location:   rec.Address,
		Value:      rec.ValueCAD,
		Description: fmt.Sprintf(
			"Work: %s | Status: %s | Applicant: %s | Contractor: %s",
			rec.WorkProposed, rec.Status, rec.Applicant, rec.Contractor,
		),
		IssuedAt: rec.IssueDate,
		RawData: map[string]any{
			"folder_number": rec.FolderNumber,
			"sub_type":      rec.SubType,
			"work_proposed": rec.WorkProposed,
			"status":        rec.Status,
			"issue_date":    rec.IssueDate.Format("2006-01-02"),
			"value_cad":     rec.ValueCAD,
			"address":       rec.Address,
			"applicant":     rec.Applicant,
			"contractor":    rec.Contractor,
		},
	}
}

// formatValue formats an int64 dollar amount with comma separators for log output.
func formatValue(n int64) string {
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// hashPermit produces a deterministic dedup key for a Richmond permit.
// Uses folder number (unique per permit) + address + date to guard against
// re-processing the same permit if it appears in multiple weekly reports.
func hashPermit(folderNumber, address string, issuedAt time.Time) string {
	h := sha256.Sum256([]byte(
		"richmond_permits|" + folderNumber + "|" + address + "|" + issuedAt.Format("2006-01-02"),
	))
	return fmt.Sprintf("%x", h)
}
