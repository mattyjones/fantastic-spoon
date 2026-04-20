// This program reads newline-separated ISBNs from a file, queries a books API
// in configurable batches with client-side rate limiting, and writes aggregated
// results as JSON (metadata + book records).
//
// Production defaults target the ISBNdb HTTP API. An OPTIONAL `.isbn_config.yml`,
// environment variables (`env.go`), and CLI flags merge in that order; CLI flags
// take precedence over file and environment values.
//
// Normative language in these comments uses the key words defined in RFC 2119
// (https://www.rfc-editor.org/rfc/rfc2119); see README.md (Requirement terminology).
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
// a successful HTTP200. The API MAY include additional fields inside each book
// object; those are preserved by decoding into map[string]interface{}.
//
// Only Total and Books are declared here; unknown top-level keys are ignored
// during decoding.
type ISBNDBResponse struct {
	Total int                      `json:"total"`
	Books []map[string]interface{} `json:"books"`
}

// Config holds runtime options for a single lookup run. The CLI fills this from
// merged `.isbn_config.yml`, environment variables, and flags; tests MAY set
// fields directly.
type Config struct {
	// APIKey is sent as the HTTP Authorization header on each batch request.
	// For ISBNdb this is usually the raw API key string (not "Bearer ...").
	APIKey string

	// InputFile is the path to a text file containing one ISBN per line.
	// Leading/trailing whitespace on the whole file is trimmed before splitting.
	InputFile string

	// OutputFile is where the combined JSON document is written (0644 permissions).
	OutputFile string

	// BatchSize is how many ISBNs to send in one API request body. It SHOULD align
	// with provider tier limits (e.g. 10 / 100 / 1000 for ISBNdb plans).
	BatchSize int

	// BooksURL is the full URL for the POST endpoint (including path, e.g. …/books).
	// Resolved in main from -api-url, ISBNDB_BOOKS_URL, or the production default.
	BooksURL string

	// RateEvery is the minimum elapsed time between batch requests. Values <= 0
	// MUST be treated as one second in runLookup so pacing is never fully disabled.
	RateEvery time.Duration

	// CollectionFile, if set, is a JSON file of accumulated books (keyed by ISBN).
	// Each run merges API results into this collection and writes a text status log.
	CollectionFile string
	// StatusLogFile is the path for the per-ISBN status log (tab-separated).
	// If empty but CollectionFile is set, defaults next to -output or next to the collection file.
	StatusLogFile string
	// BookURLTemplate is printf format with one %s for ISBN when the API record has no URL.
	BookURLTemplate string
}

// main loads an OPTIONAL .isbn_config.yml (see config.go), applies environment
// overrides, then parses CLI flags (which override file and env). Non-validation
// failures use log.Fatal (exit code 1); usage errors print a message and help.
func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	base, err := mergeAppConfig(wd)
	if err != nil {
		log.Fatal(err)
	}

	config := base.Config
	webUI := base.WebUI
	listen := base.Listen
	readTimeout := base.ReadTimeout
	writeTimeout := base.WriteTimeout
	idleTimeout := base.IdleTimeout

	// --- Flags: I/O, credentials, batching, and overrides (defaults from file + env) ---
	flag.StringVar(&config.InputFile, "input", config.InputFile, "Path to the line-separated ISBN file (REQUIRED unless -web)")
	flag.StringVar(&config.OutputFile, "output", config.OutputFile, "Output JSON file path")
	flag.StringVar(&config.APIKey, "key", config.APIKey, "API key (or set "+EnvISBNAPKey+" in the environment)")
	flag.IntVar(&config.BatchSize, "batch", config.BatchSize, "Batch size (Academic: 10, Basic: 100, Pro: 1000)")
	flag.StringVar(&config.BooksURL, "api-url", config.BooksURL, "Books API POST URL (default: "+EnvBooksURL+", ISBNDB_BOOKS_URL, or production ISBNdb)")
	flag.DurationVar(&config.RateEvery, "rate-every", config.RateEvery, "Minimum time between API batch requests")
	flag.BoolVar(&webUI, "web", webUI, "Run a local web UI to paste ISBNs or upload a file")
	flag.StringVar(&listen, "listen", listen, "Listen address for -web (localhost only by default)")
	flag.DurationVar(&readTimeout, "read-timeout", readTimeout, "Web server max duration for reading the entire request (for -web)")
	flag.DurationVar(&writeTimeout, "write-timeout", writeTimeout, "Web server max duration for writing the response (for -web)")
	flag.DurationVar(&idleTimeout, "idle-timeout", idleTimeout, "Web server keep-alive timeout for idle connections (for -web)")
	flag.StringVar(&config.CollectionFile, "collection", config.CollectionFile, "JSON collection file to merge books into (enables per-ISBN status log)")
	flag.StringVar(&config.StatusLogFile, "status-log", config.StatusLogFile, "Text log path (ISBN, status, outcome, URL); default derived from -output or -collection")
	flag.StringVar(&config.BookURLTemplate, "book-url-template", config.BookURLTemplate, "Printf template for book URL when API omits one (one %s = ISBN)")
	flag.Parse()

	config.BooksURL = finalizeBooksURL(config.BooksURL)

	if webUI {
		if err := startWebUI(listen, config, readTimeout, writeTimeout, idleTimeout); err != nil {
			log.Fatal(err)
		}
		return
	}

	if config.InputFile == "" || config.APIKey == "" {
		fmt.Println("Error: MUST specify input file and API key for CLI mode.")
		flag.Usage()
		os.Exit(1)
	}

	if err := run(context.Background(), config, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run reads ISBN lines from cfg.InputFile, runs the lookup pipeline, and writes JSON to cfg.OutputFile.
func run(ctx context.Context, cfg Config, out io.Writer) error {
	if cfg.InputFile == "" || cfg.APIKey == "" {
		return fmt.Errorf("MUST specify input file and API key for CLI mode")
	}
	data, err := os.ReadFile(cfg.InputFile)
	if err != nil {
		return fmt.Errorf("read input file: %w", err)
	}

	// One ISBN per line; TrimSpace removes a trailing newline so a final blank
	// line does not create an extra empty element in most files.
	allISBNs := strings.Split(strings.TrimSpace(string(data)), "\n")

	output, err := runLookup(ctx, cfg, allISBNs, out)
	if err != nil {
		return err
	}

	file, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	if err := os.WriteFile(cfg.OutputFile, file, 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	meta, _ := output["metadata"].(map[string]interface{})
	total, _ := meta["total_found"].(int)
	fmt.Fprintf(out, "Done! Saved %d books to %s\n", total, cfg.OutputFile)
	if cfg.CollectionFile != "" {
		if sm, ok := output["metadata"].(map[string]interface{}); ok {
			if p, ok := sm["status_log_path"].(string); ok {
				fmt.Fprintf(out, "Collection updated at %s; status log: %s\n", cfg.CollectionFile, p)
			}
		}
	}
	return nil
}

// runLookup calls the books API in batches with rate limiting and returns the
// same JSON-serializable document as the CLI (metadata + books). Progress is
// written to out; batch errors are logged and skipped.
// If cfg.CollectionFile is set, merges into the collection and writes a status log.
func runLookup(ctx context.Context, cfg Config, allISBNs []string, out io.Writer) (map[string]interface{}, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key MUST be set (non-empty)")
	}
	if cfg.BooksURL == "" {
		cfg.BooksURL = "https://api2.isbndb.com/books"
	}
	tpl := cfg.BookURLTemplate
	if tpl == "" {
		tpl = DefaultBookURLTemplate
	}

	var coll map[string]map[string]interface{}
	var statusLines []string
	if cfg.CollectionFile != "" {
		var err error
		coll, err = loadCollection(cfg.CollectionFile)
		if err != nil {
			return nil, err
		}
		statusLines = make([]string, 0, len(allISBNs))
	}

	rateEvery := cfg.RateEvery
	if rateEvery <= 0 {
		rateEvery = time.Second
	}

	limiter := rate.NewLimiter(rate.Every(rateEvery), 1)
	client := &http.Client{Timeout: 30 * time.Second}
	var finalBooks []map[string]interface{}
	var addedN, dupN, notAddedN int

	fmt.Fprintf(out, "Processing %d ISBNs in batches of %d...\n", len(allISBNs), cfg.BatchSize)

	for i := 0; i < len(allISBNs); i += cfg.BatchSize {
		end := i + cfg.BatchSize
		if end > len(allISBNs) {
			end = len(allISBNs)
		}
		batch := allISBNs[i:end]

		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}

		fmt.Fprintf(out, "Requesting batch %d to %d...\n", i+1, end)
		books, err := lookupBatch(client, cfg.BooksURL, cfg.APIKey, batch)
		if err != nil {
			log.Printf("Error processing batch starting at %d: %v", i, err)
			if coll != nil {
				for _, rawISBN := range batch {
					statusLines = append(statusLines, formatStatusLogLine(rawISBN, StatusNotAdded, ""))
					notAddedN++
				}
			}
			continue
		}
		finalBooks = append(finalBooks, books...)

		if coll != nil {
			byISBN := indexBooksByISBN(books)
			for _, rawISBN := range batch {
				isbn := normalizeISBN(rawISBN)
				if isbn == "" {
					statusLines = append(statusLines, formatStatusLogLine(rawISBN, StatusNotAdded, ""))
					notAddedN++
					continue
				}
				b, ok := byISBN[isbn]
				if !ok {
					statusLines = append(statusLines, formatStatusLogLine(isbn, StatusNotAdded, ""))
					notAddedN++
					continue
				}
				url := bookURL(b, isbn, tpl)
				if _, exists := coll[isbn]; exists {
					statusLines = append(statusLines, formatStatusLogLine(isbn, StatusDuplicate, url))
					dupN++
				} else {
					coll[isbn] = cloneBookMap(b)
					statusLines = append(statusLines, formatStatusLogLine(isbn, StatusAdded, url))
					addedN++
				}
			}
		}
	}

	meta := map[string]interface{}{
		"processed_at": time.Now().Format(time.RFC3339),
		"total_found":  len(finalBooks),
	}
	if coll != nil {
		meta["collection_added"] = addedN
		meta["collection_duplicates"] = dupN
		meta["collection_not_added"] = notAddedN
		if err := saveCollection(cfg.CollectionFile, coll); err != nil {
			return nil, fmt.Errorf("write collection: %w", err)
		}
		logPath := cfg.StatusLogFile
		if logPath == "" {
			if cfg.OutputFile != "" {
				logPath = defaultStatusLogPath(cfg.OutputFile)
			} else {
				logPath = defaultStatusLogFromCollection(cfg.CollectionFile)
			}
		}
		if err := writeStatusLog(logPath, statusLines); err != nil {
			return nil, fmt.Errorf("write status log: %w", err)
		}
		meta["status_log_path"] = logPath
	}

	return map[string]interface{}{
		"metadata": meta,
		"books":    finalBooks,
	}, nil
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
//   - 200: body decoded as ISBNDBResponse; returned slice is result.Books (MAY be nil).
//   - 429: returned error mentions rate limiting (caller MAY log and continue).
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
