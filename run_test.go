package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRun_success_singleBatch(t *testing.T) {
	t.Parallel()

	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})

	dir := t.TempDir()
	inPath := filepath.Join(dir, "isbn.txt")
	outPath := filepath.Join(dir, "out.json")
	mustWriteFile(t, inPath, "9781111111111\n9782222222222\n")

	var stdout bytes.Buffer
	err := run(context.Background(), Config{
		InputFile:  inPath,
		OutputFile: outPath,
		BatchSize:  100,
		APIKey:     "k",
		BooksURL:   srv.URL + "/books",
		RateEvery:  time.Nanosecond,
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}

	out := mustReadJSONOutput(t, outPath)
	if out.Metadata.TotalFound != 2 {
		t.Fatalf("total_found = %d, want 2", out.Metadata.TotalFound)
	}
	if len(out.Books) != 2 {
		t.Fatalf("len(books) = %d", len(out.Books))
	}
	if out.Books[0]["title"] != "Title-9781111111111" {
		t.Fatalf("book[0] = %#v", out.Books[0])
	}
	if !strings.Contains(stdout.String(), "Processing 2 ISBNs") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Done! Saved 2 books") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_success_multipleBatches(t *testing.T) {
	t.Parallel()

	srv, fake := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "secret"})

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.txt")
	outPath := filepath.Join(dir, "out.json")
	mustWriteFile(t, inPath, "a\nb\nc\nd\n")

	err := run(context.Background(), Config{
		InputFile:  inPath,
		OutputFile: outPath,
		BatchSize:  2,
		APIKey:     "secret",
		BooksURL:   srv.URL + "/books",
		RateEvery:  time.Nanosecond,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if fake.BatchCount() != 2 {
		t.Fatalf("batch requests = %d, want 2", fake.BatchCount())
	}

	out := mustReadJSONOutput(t, outPath)
	if out.Metadata.TotalFound != 4 {
		t.Fatalf("total_found = %d, want 4", out.Metadata.TotalFound)
	}
}

func TestRun_continuesWhenBatchFails(t *testing.T) {
	t.Parallel()

	discardLogOutput(t)

	srv, fake := newFakeISBNServer(t, fakeISBNServerOpts{
		APIKey:      "k",
		FailOnBatch: 2,
	})

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.txt")
	outPath := filepath.Join(dir, "out.json")
	mustWriteFile(t, inPath, "x\ny\nz\nw\n")

	err := run(context.Background(), Config{
		InputFile:  inPath,
		OutputFile: outPath,
		BatchSize:  2,
		APIKey:     "k",
		BooksURL:   srv.URL + "/books",
		RateEvery:  time.Nanosecond,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if fake.BatchCount() != 2 {
		t.Fatalf("batch requests = %d, want 2", fake.BatchCount())
	}

	out := mustReadJSONOutput(t, outPath)
	if out.Metadata.TotalFound != 2 {
		t.Fatalf("total_found = %d, want 2 (first batch only)", out.Metadata.TotalFound)
	}
}

func TestRun_rejectsEmptyConfig(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), Config{
		InputFile:  "",
		OutputFile: t.TempDir() + "/o.json",
		APIKey:     "k",
		BooksURL:   "http://localhost/books",
		RateEvery:  time.Nanosecond,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "MUST") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_readInputError(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), Config{
		InputFile:  filepath.Join(t.TempDir(), "nope.txt"),
		OutputFile: filepath.Join(t.TempDir(), "out.json"),
		APIKey:     "k",
		BooksURL:   "http://localhost/books",
		RateEvery:  time.Nanosecond,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "read input") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_collection_addThenDuplicate(t *testing.T) {
	t.Parallel()

	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.txt")
	outPath := filepath.Join(dir, "out.json")
	collPath := filepath.Join(dir, "collection.json")
	logPath := filepath.Join(dir, "run.status.log")
	mustWriteFile(t, inPath, "9781111111111\n9782222222222\n")

	cfg := Config{
		InputFile:      inPath,
		OutputFile:     outPath,
		BatchSize:      10,
		APIKey:         "k",
		BooksURL:       srv.URL + "/books",
		RateEvery:      time.Nanosecond,
		CollectionFile: collPath,
		StatusLogFile:  logPath,
	}
	if err := run(context.Background(), cfg, io.Discard); err != nil {
		t.Fatal(err)
	}

	log1 := mustReadFile(t, logPath)
	if !strings.Contains(log1, "9781111111111\tadded\tsuccessful\thttps://example.test/book/9781111111111") {
		t.Fatalf("log1 = %q", log1)
	}
	if !strings.Contains(log1, "9782222222222\tadded\tsuccessful\thttps://example.test/book/9782222222222") {
		t.Fatalf("log1 = %q", log1)
	}

	doc1 := mustReadCollection(t, collPath)
	if len(doc1.Books) != 2 {
		t.Fatalf("collection size = %d", len(doc1.Books))
	}

	if err := run(context.Background(), cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	log2 := mustReadFile(t, logPath)
	if !strings.Contains(log2, "\tduplicate\tsuccessful\thttps://example.test/book/9781111111111") {
		t.Fatalf("log2 = %q", log2)
	}
	if !strings.Contains(log2, "\tduplicate\tsuccessful\thttps://example.test/book/9782222222222") {
		t.Fatalf("log2 = %q", log2)
	}
	if strings.Contains(log2, "\tadded\t") {
		t.Fatalf("second run MUST NOT add again: %q", log2)
	}
	doc2 := mustReadCollection(t, collPath)
	if len(doc2.Books) != 2 {
		t.Fatalf("collection size after dup run = %d", len(doc2.Books))
	}
}

func TestRun_collection_batchFailureMarksNotAdded(t *testing.T) {
	t.Parallel()
	discardLogOutput(t)

	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k", FailOnBatch: 2})
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.txt")
	outPath := filepath.Join(dir, "out.json")
	collPath := filepath.Join(dir, "collection.json")
	logPath := filepath.Join(dir, "s.log")
	mustWriteFile(t, inPath, "a\nb\nc\nd\n")

	err := run(context.Background(), Config{
		InputFile:      inPath,
		OutputFile:     outPath,
		BatchSize:      2,
		APIKey:         "k",
		BooksURL:       srv.URL + "/books",
		RateEvery:      time.Nanosecond,
		CollectionFile: collPath,
		StatusLogFile:  logPath,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	logText := mustReadFile(t, logPath)
	if !strings.Contains(logText, "c\tnot_added\tunsuccessful\t") || !strings.Contains(logText, "d\tnot_added\tunsuccessful\t") {
		t.Fatalf("log = %q", logText)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustReadCollection(t *testing.T, path string) CollectionFileJSON {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc CollectionFileJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// --- test helpers ---

type outputFile struct {
	Metadata struct {
		ProcessedAt string `json:"processed_at"`
		TotalFound  int    `json:"total_found"`
	} `json:"metadata"`
	Books []map[string]interface{} `json:"books"`
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustReadJSONOutput(t *testing.T, path string) outputFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var o outputFile
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatalf("json: %v\n%s", err, raw)
	}
	return o
}

type fakeISBNServerOpts struct {
	APIKey      string
	FailOnBatch int
}

type fakeISBNServer struct {
	mu         sync.Mutex
	batchCount int
}

func (f *fakeISBNServer) BatchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.batchCount
}

func newFakeISBNServer(t *testing.T, opts fakeISBNServerOpts) (*httptest.Server, *fakeISBNServer) {
	t.Helper()
	f := &fakeISBNServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/books" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if opts.APIKey != "" && r.Header.Get("Authorization") != opts.APIKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		list := strings.Split(payload["isbns"], ",")

		f.mu.Lock()
		f.batchCount++
		call := f.batchCount
		f.mu.Unlock()

		if opts.FailOnBatch > 0 && call == opts.FailOnBatch {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}

		books := make([]map[string]interface{}, 0, len(list))
		for _, isbn := range list {
			isbn = strings.TrimSpace(isbn)
			if isbn == "" {
				continue
			}
			books = append(books, map[string]interface{}{
				"isbn":  isbn,
				"title": "Title-" + isbn,
				"url":   "https://example.test/book/" + isbn,
			})
		}
		_ = json.NewEncoder(w).Encode(ISBNDBResponse{Total: len(books), Books: books})
	}))
	t.Cleanup(srv.Close)
	return srv, f
}

func discardLogOutput(t *testing.T) {
	t.Helper()
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })
}
