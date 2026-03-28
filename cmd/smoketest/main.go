// smoketest is a dev-only tool that runs the Richmond collector against the
// live City of Richmond website and pretty-prints the results as JSON.
//
// Usage:
//
//	go run ./cmd/smoketest/          — full run, JSON output
//	go run ./cmd/smoketest/ -rawpdf  — print raw PDF text lines (debug parsing)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alvindcastro/blockscout/internal/collector"
)

var rawPDF = flag.Bool("rawpdf", false, "print raw text extracted from the latest PDF and exit")

func main() {
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("[smoketest] ")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if *rawPDF {
		dumpRawPDF(ctx)
		return
	}

	c := collector.NewRichmondCollector()
	c.Verbose = true
	log.Printf("running collector: %s", c.Name())

	start := time.Now()
	projects, err := c.Collect(ctx)
	if err != nil {
		log.Fatalf("collect failed: %v", err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)

	if len(projects) == 0 {
		log.Println("no projects returned — check filter thresholds or PDF structure")
		os.Exit(0)
	}

	var totalValue int64
	for _, p := range projects {
		totalValue += p.Value
	}
	log.Printf("found %d permits in %s — total value $%s CAD",
		len(projects), elapsed, formatCAD(totalValue))
	fmt.Println()

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(projects); err != nil {
		log.Fatalf("json encode: %v", err)
	}
}

// dumpRawPDF downloads the latest Richmond PDF and prints the raw text extracted
// by pdftotext. Used to diagnose parsing issues and verify the line format the
// parser will receive.
func dumpRawPDF(ctx context.Context) {
	const reportsURL = "https://www.richmond.ca/business-development/building-approvals/reports/weeklyreports.htm"
	const baseURL = "https://www.richmond.ca"

	client := &http.Client{Timeout: 30 * time.Second}

	// Fetch reports page to get first PDF URL
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reportsURL, nil)
	req.Header.Set("User-Agent", "blockscout-leadgen/1.0")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("fetch reports page: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Find first building report PDF link
	marker := `/__shared/assets/buildingreport`
	idx := strings.Index(string(body), marker)
	if idx == -1 {
		log.Fatal("no building report PDF link found on page")
	}
	end := strings.Index(string(body)[idx:], `"`)
	pdfPath := string(body)[idx : idx+end]
	pdfURL := baseURL + pdfPath
	log.Printf("downloading: %s", pdfURL)

	// Download PDF to temp file
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	req2.Header.Set("User-Agent", "blockscout-leadgen/1.0")
	resp2, err := client.Do(req2)
	if err != nil {
		log.Fatalf("download pdf: %v", err)
	}
	tmp, _ := os.CreateTemp("", "richmond-*.pdf")
	io.Copy(tmp, resp2.Body)
	resp2.Body.Close()
	tmp.Close()
	defer os.Remove(tmp.Name())

	// Locate pdftotext
	pdftotext := `C:\Program Files\Git\mingw64\bin\pdftotext.exe`
	if _, err := os.Stat(pdftotext); err != nil {
		if p, lerr := exec.LookPath("pdftotext"); lerr == nil {
			pdftotext = p
		} else {
			log.Fatalf("pdftotext not found — install Poppler or Git for Windows")
		}
	}

	out, err := exec.Command(pdftotext, tmp.Name(), "-").Output()
	if err != nil {
		log.Fatalf("pdftotext: %v", err)
	}

	fmt.Println("─────────────────────────── RAW PDF TEXT (pdftotext) ───────────────────────────")
	fmt.Print(string(out))
}

func formatCAD(n int64) string {
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
