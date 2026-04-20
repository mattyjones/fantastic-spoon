package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBareWebHandler() *webHandler {
	return &webHandler{defaults: defaultAppConfig().Config}
}

func TestWebStatic_embeddedIndexHTML(t *testing.T) {
	t.Parallel()
	if _, err := webStatic.ReadFile("web/index.html"); err != nil {
		t.Fatal(err)
	}
}

func TestWebIndexHasNoAPIKeyInBrowser(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(strings.ToLower(s), "api_key") ||
		strings.Contains(s, `type="password"`) ||
		strings.Contains(s, `"api_key"`) {
		t.Fatal("web UI MUST NOT collect or send api_key; use ISBN_AP_KEY on the server only")
	}
}

func TestHandleWebIndex_GET(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handleWebIndex(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "fantastic-spoon") || !strings.Contains(body, "<textarea") {
		t.Fatalf("unexpected HTML: %s", truncateForLog(body, 400))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestHandleWebIndex_wrongMethod(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handleWebIndex(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleWebLookup_success(t *testing.T) {
	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})
	t.Setenv("ISBN_AP_KEY", "k")
	t.Setenv("ISBNDB_BOOKS_URL", srv.URL+"/books")

	payload := map[string]interface{}{
		"isbns":          "9781111111111\n9782222222222",
		"batch_size":     100,
		"rate_every_sec": 1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var got webLookupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Log == "" || !strings.Contains(got.Log, "Processing 2 ISBNs") {
		t.Fatalf("log = %q", got.Log)
	}
	meta, ok := got.Data["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata: %#v", got.Data["metadata"])
	}
	tf, ok := meta["total_found"].(float64)
	if !ok || int(tf) != 2 {
		t.Fatalf("total_found = %v (%T)", meta["total_found"], meta["total_found"])
	}
	books, ok := got.Data["books"].([]interface{})
	if !ok || len(books) != 2 {
		t.Fatalf("books = %#v", got.Data["books"])
	}
}

func TestHandleWebLookup_usesExplicitAPIURL(t *testing.T) {
	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "secret"})
	t.Setenv("ISBN_AP_KEY", "secret")
	t.Setenv("ISBNDB_BOOKS_URL", "http://127.0.0.1:9/nope")

	payload := map[string]interface{}{
		"isbns":          "one",
		"batch_size":     10,
		"rate_every_sec": 1,
		"api_url":        srv.URL + "/books",
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWebLookup_ignoresAPIKeyInJSONBody(t *testing.T) {
	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})
	t.Setenv("ISBN_AP_KEY", "k")
	t.Setenv("ISBNDB_BOOKS_URL", srv.URL+"/books")

	// Spoofed api_key MUST NOT override env (decoder ignores unknown fields; server uses env only).
	payload := `{"isbns":"9781111111111","api_key":"wrong","batch_size":10,"rate_every_sec":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWebLookup_missingAPIKey(t *testing.T) {
	t.Setenv("ISBN_AP_KEY", "")
	payload := `{"isbns":"9781111111111","batch_size":10,"rate_every_sec":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBody["error"], "ISBN_AP_KEY") {
		t.Fatalf("error = %q", errBody["error"])
	}
}

func TestHandleWebLookup_withCollectionWritesStatusLog(t *testing.T) {
	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})
	t.Setenv("ISBN_AP_KEY", "k")
	t.Setenv("ISBNDB_BOOKS_URL", srv.URL+"/books")
	dir := t.TempDir()
	collPath := filepath.Join(dir, "collection.json")
	logPath := filepath.Join(dir, "st.log")
	payload := map[string]interface{}{
		"isbns":           "9781111111111",
		"batch_size":      10,
		"rate_every_sec":  1,
		"collection_file": collPath,
		"status_log_file": logPath,
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	logText, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logText), "9781111111111\tadded\tsuccessful\t") {
		t.Fatalf("log = %q", logText)
	}
}

func TestHandleWebLookup_usesEnvAPIKey(t *testing.T) {
	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "envk"})
	t.Setenv("ISBN_AP_KEY", "envk")
	t.Setenv("ISBNDB_BOOKS_URL", srv.URL+"/books")

	payload := `{"isbns":"a\nb","batch_size":10,"rate_every_sec":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWebLookup_emptyISBNs(t *testing.T) {
	t.Setenv("ISBN_AP_KEY", "k")
	payload := `{"isbns":"  \n  ","batch_size":10,"rate_every_sec":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleWebLookup_invalidJSON(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleWebLookup_wrongMethod(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/lookup", nil)
	rec := httptest.NewRecorder()
	testBareWebHandler().handleWebLookup(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleWebLookup_usesServerDefaultsForBatchAndRate(t *testing.T) {
	srv, fake := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})
	t.Setenv("ISBN_AP_KEY", "k")
	t.Setenv("ISBNDB_BOOKS_URL", "")

	h := &webHandler{defaults: Config{
		BatchSize: 2,
		BooksURL:  srv.URL + "/books",
		RateEvery: time.Nanosecond,
	}}
	payload := `{"isbns":"a\nb\nc\nd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/lookup", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.handleWebLookup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if fake.BatchCount() != 2 {
		t.Fatalf("batch requests = %d, want 2", fake.BatchCount())
	}
}

func TestRunLookup_rejectsEmptyAPIKey(t *testing.T) {
	t.Parallel()
	_, err := runLookup(context.Background(), Config{
		APIKey:    "",
		BooksURL:  "http://localhost/books",
		BatchSize: 10,
		RateEvery: 0,
	}, []string{"1"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v", err)
	}
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
