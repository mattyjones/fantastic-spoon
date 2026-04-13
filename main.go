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

// ISBNDBResponse mirrors the expected API response structure
type ISBNDBResponse struct {
	Total int                      `json:"total"`
	Books []map[string]interface{} `json:"books"`
}

type Config struct {
	APIKey     string
	InputFile  string
	OutputFile string
	BatchSize  int
	BooksURL   string
	RateEvery  time.Duration
}

func main() {
	config := Config{}
	var apiURL string
	flag.StringVar(&config.InputFile, "input", "", "Path to the line-separated ISBN file (required)")
	flag.StringVar(&config.OutputFile, "output", "results.json", "Output JSON file path")
	flag.StringVar(&config.APIKey, "key", os.Getenv("ISBNDB_API_KEY"), "ISBNDB API Key (or use ISBNDB_API_KEY env var)")
	flag.IntVar(&config.BatchSize, "batch", 100, "Batch size (Academic: 10, Basic: 100, Pro: 1000)")
	flag.StringVar(&apiURL, "api-url", "", "Books API POST URL (default: ISBNDB_BOOKS_URL env or production ISBNdb)")
	flag.DurationVar(&config.RateEvery, "rate-every", time.Second, "Minimum time between API batch requests")
	flag.Parse()

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

func run(ctx context.Context, cfg Config, out io.Writer) error {
	if cfg.InputFile == "" || cfg.APIKey == "" {
		return fmt.Errorf("input file and API key are required")
	}
	if cfg.BooksURL == "" {
		cfg.BooksURL = "https://api2.isbndb.com/books"
	}
	rateEvery := cfg.RateEvery
	if rateEvery <= 0 {
		rateEvery = time.Second
	}

	data, err := os.ReadFile(cfg.InputFile)
	if err != nil {
		return fmt.Errorf("read input file: %w", err)
	}
	allISBNs := strings.Split(strings.TrimSpace(string(data)), "\n")

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

func lookupBatch(client *http.Client, booksURL, apiKey string, isbns []string) ([]map[string]interface{}, error) {
	return lookupBatchTo(client, booksURL, apiKey, isbns)
}

func lookupBatchTo(client *http.Client, url string, apiKey string, isbns []string) ([]map[string]interface{}, error) {
	// Prepare JSON payload
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
