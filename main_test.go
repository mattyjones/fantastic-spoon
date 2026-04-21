package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLookupBatchTo_success(t *testing.T) {
	t.Parallel()

	const wantKey = "test-api-key"
	wantISBNs := []string{"9780143127741", "9780316769488"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/books" {
			t.Errorf("path = %q, want /books", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if r.Header.Get("Authorization") != wantKey {
			t.Errorf("Authorization header mismatch")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got := payload["isbns"]; got != strings.Join(wantISBNs, ",") {
			t.Errorf("isbns payload = %q, want %q", got, strings.Join(wantISBNs, ","))
		}

		_ = json.NewEncoder(w).Encode(ISBNDBResponse{
			Total: 2,
			Books: []map[string]interface{}{
				{"isbn": wantISBNs[0], "title": "Book A"},
				{"isbn": wantISBNs[1], "title": "Book B"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	books, err := lookupBatchTo(srv.Client(), srv.URL+"/books", wantKey, wantISBNs)
	if err != nil {
		t.Fatalf("lookupBatchTo: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("len(books) = %d, want 2", len(books))
	}
	if books[0]["title"] != "Book A" {
		t.Errorf("first book title = %v", books[0]["title"])
	}
}

func TestLookupBatchTo_rateLimited(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	_, err := lookupBatchTo(srv.Client(), srv.URL+"/books", "k", []string{"1"})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v, want rate limited error", err)
	}
}

func TestLookupBatchTo_apiErrorIncludesBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request detail"))
	}))
	t.Cleanup(srv.Close)

	_, err := lookupBatchTo(srv.Client(), srv.URL+"/books", "k", []string{"1"})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad request detail") {
		t.Fatalf("err = %v, want API error with body", err)
	}
}

func TestLookupBatchTo_invalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	_, err := lookupBatchTo(srv.Client(), srv.URL+"/books", "k", []string{"1"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestLookupBatchTo_emptyBooksArray(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ISBNDBResponse{Total: 0, Books: nil})
	}))
	t.Cleanup(srv.Close)

	books, err := lookupBatchTo(srv.Client(), srv.URL+"/books", "k", []string{"999"})
	if err != nil {
		t.Fatalf("lookupBatchTo: %v", err)
	}
	if len(books) != 0 {
		t.Fatalf("len(books) = %d, want 0", len(books))
	}
}

func TestLookupBatchTo_clientTransportError(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		}),
	}

	_, err := lookupBatchTo(client, "https://example.invalid/books", "k", []string{"1"})
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("err = %v, want network error", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
