// This program reads newline-separated ISBNs from a file, queries a books API
// in configurable batches with client-side rate limiting, and writes aggregated
// results as JSON (metadata + book records).
//
// Production defaults target the ISBNdb HTTP API; flags and environment variables
// allow overriding the endpoint and pacing for tests or alternate deployments.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// ISBNDBResponse is the JSON shape returned by the books lookup endpoint after
// a successful HTTP200. The API may include additional fields inside each book
// object; those are preserved by decoding into map[string]interface{}.
//
// Only Total and Books are declared here; unknown top-level keys are ignored
// during decoding.
type ISBNDBResponse struct {
	Total int                      `json:"total"`
	Books []map[string]interface{} `json:"books"`
}

// Config holds runtime options for a single lookup run. Values are typically
// filled from CLI flags in main; tests may construct Config directly.
type Config struct {
	// APIKey is sent as the HTTP Authorization header on each batch request.
	// For ISBNdb this is usually the raw API key string (not "Bearer ...").
	APIKey string

	// InputFile is the path to a text file containing one ISBN per line.
	// Leading/trailing whitespace on the whole file is trimmed before splitting.
	InputFile string

	// OutputFile is where the combined JSON document is written (0644 permissions).
	OutputFile string

	// BatchSize is how many ISBNs to send in one API request body. Must align
	// with provider tier limits (e.g. 10 / 100 / 1000 for ISBNdb plans).
	BatchSize int

	// BooksURL is the full URL for the POST endpoint (including path, e.g. …/books).
	// Resolved in main from -api-url, ISBNDB_BOOKS_URL, or the production default.
	BooksURL string

	// RateEvery is the minimum elapsed time between batch requests. Values <= 0
	// are treated as one second inside run so callers cannot accidentally disable pacing.
	RateEvery time.Duration
}

// main parses CLI flags, resolves the API URL and pacing, validates required
// settings, then delegates to run. Non-validation failures are printed with
// log.Fatal (exit code 1); usage errors print a message and flag help first.
func main() {
	config := Config{}
	var apiURL string

	// --- Flags: I/O, credentials, batching, and overrides ---
	flag.StringVar(&config.InputFile, "input", "", "Path to the line-separated ISBN file (required)")
	flag.StringVar(&config.OutputFile, "output", "results.json", "Output JSON file path")
	flag.StringVar(&config.APIKey, "key", os.Getenv("ISBNDB_API_KEY"), "ISBNDB API Key (or use ISBNDB_API_KEY env var)")
	flag.IntVar(&config.BatchSize, "batch", 100, "Batch size (Academic: 10, Basic: 100, Pro: 1000)")
	flag.StringVar(&apiURL, "api-url", "", "Books API POST URL (default: ISBNDB_BOOKS_URL env or production ISBNdb)")
	flag.DurationVar(&config.RateEvery, "rate-every", time.Second, "Minimum time between API batch requests")
	flag.Parse()

	// Resolve BooksURL: explicit flag wins, then env, then hard-coded ISBNdb URL.
	// This ordering lets operators override per run without changing code, and
	// lets integration tests point at httptest servers via -api-url or env.
	if apiURL != "" {
		config.BooksURL = apiURL
	} else if v := os.Getenv("ISBNDB_BOOKS_URL"); v != "" {
		config.BooksURL = v
	} else {
		config.BooksURL = "https://api2.isbndb.com/books"
	}

	if config.InputFile == "" || config.APIKey == "" {
		fmt.Println("Error: Input file and API Key are required.")
		flag.Usage()
		os.Exit(1)
	}

	if err := run(context.Background(), config, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run executes the full pipeline: read ISBNs, call the API in batches with
// rate limiting, merge successful responses, and write JSON to OutputFile.
//
// Progress messages are written to out (main passes os.Stdout). Batch errors
// are logged to the standard logger and skipped so one failed batch does not
// abort the entire run; the output file still reflects all successfully fetched books.
//
// Context cancellation: limiter.Wait respects ctx; if ctx is cancelled, run
// returns that error and may leave OutputFile from a previous run untouched.
func run(ctx context.Context, cfg Config, out io.Writer) error {
	if cfg.InputFile == "" || cfg.APIKey == "" {
		return fmt.Errorf("input file and API key are required")
	}
	if cfg.BooksURL == "" {
		cfg.BooksURL = "https://api2.isbndb.com/books"
	}

	// Normalize pacing: zero or negative durations would confuse rate.Every;
	// default to one second between batches (same as the CLI default).
	rateEvery := cfg.RateEvery
	if rateEvery <= 0 {
		rateEvery = time.Second
	}

	data, err := os.ReadFile(cfg.InputFile)
	if err != nil {
		return fmt.Errorf("read input file: %w", err)
	}

	// One ISBN per line; TrimSpace removes a trailing newline so a final blank
	// line does not create an extra empty element in most files.
	allISBNs := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Token bucket: burst 1 enforces a minimum interval of rateEvery between
	// successive Wait calls (one batch request per Wait).
	limiter := rate.NewLimiter(rate.Every(rateEvery), 1)
	client := &http.Client{Timeout: 30 * time.Second}
	var finalBooks []map[string]interface{}

	fmt.Fprintf(out, "Processing %d ISBNs in batches of %d...\n", len(allISBNs), cfg.BatchSize)

	for i := 0; i < len(allISBNs); i += cfg.BatchSize {
		end := i + cfg.BatchSize
		if end > len(allISBNs) {
			end = len(allISBNs)
		}
		batch := allISBNs[i:end]

		if err := limiter.Wait(ctx); err != nil {
			return err
		}

		fmt.Fprintf(out, "Requesting batch %d to %d...\n", i+1, end)
		books, err := lookupBatch(client, cfg.BooksURL, cfg.APIKey, batch)
		if err != nil {
			log.Printf("Error processing batch starting at %d: %v", i, err)
			continue
		}
		finalBooks = append(finalBooks, books...)
	}

	output := map[string]interface{}{
		"metadata": map[string]interface{}{
			"processed_at": time.Now().Format(time.RFC3339),
			"total_found":  len(finalBooks),
		},
		"books": finalBooks,
	}

	file, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	if err := os.WriteFile(cfg.OutputFile, file, 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Fprintf(out, "Done! Saved %d books to %s\n", len(finalBooks), cfg.OutputFile)
	return nil
}

// lookupBatch performs a single multi-ISBN lookup against booksURL using the
// shared HTTP client. It is a thin wrapper around lookupBatchTo so production
// and tests can share one implementation while tests inject arbitrary URLs.
func lookupBatch(client *http.Client, booksURL, apiKey string, isbns []string) ([]map[string]interface{}, error) {
	return lookupBatchTo(client, booksURL, apiKey, isbns)
}

// lookupBatchTo sends a POST request matching the ISBNdb-style contract:
//
//   - URL: caller-provided (production https://api2.isbndb.com/books or test double).
//   - Body: JSON object {"isbns": "comma-separated list"}.
//   - Headers: Content-Type application/json; Authorization set to apiKey.
//
// Responses:
//   - 200: body decoded as ISBNDBResponse; returned slice is result.Books (may be nil).
//   - 429: returned error mentions rate limiting (caller may log and continue).
//   - Other: error includes status code and response body snippet for debugging.
//
// Network and JSON decode errors are returned as-is or wrapped by the standard library.
func lookupBatchTo(client *http.Client, url string, apiKey string, isbns []string) ([]map[string]interface{}, error) {
	payload := map[string]string{"isbns": strings.Join(isbns, ",")}
	jsonBody, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var result ISBNDBResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Books, nil
}
