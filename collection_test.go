package main

import (
	"strings"
	"testing"
)

func TestFormatStatusLogLine(t *testing.T) {
	t.Parallel()
	got := formatStatusLogLine("9781111111111", StatusAdded, "https://x/y")
	if got != "9781111111111\tadded\tsuccessful\thttps://x/y" {
		t.Fatalf("got %q", got)
	}
	got = formatStatusLogLine("9782222222222", StatusDuplicate, "https://z")
	if got != "9782222222222\tduplicate\tsuccessful\thttps://z" {
		t.Fatalf("got %q", got)
	}
	got = formatStatusLogLine("9783333333333", StatusNotAdded, "")
	if got != "9783333333333\tnot_added\tunsuccessful\t" {
		t.Fatalf("got %q", got)
	}
}

func TestBookURL_prefersAPIField(t *testing.T) {
	t.Parallel()
	b := map[string]interface{}{"isbn": "1", "url": "https://api/u"}
	if u := bookURL(b, "1", "https://fallback/%s"); u != "https://api/u" {
		t.Fatalf("got %q", u)
	}
}

func TestBookURL_templateFallback(t *testing.T) {
	t.Parallel()
	b := map[string]interface{}{"isbn": "999"}
	if u := bookURL(b, "999", "https://ex/%s"); u != "https://ex/999" {
		t.Fatalf("got %q", u)
	}
}

func TestNormalizeISBN(t *testing.T) {
	t.Parallel()
	if normalizeISBN("  978 \n") != "978" {
		t.Fatal(normalizeISBN("  978 \n"))
	}
}

func TestIndexBooksByISBN(t *testing.T) {
	t.Parallel()
	books := []map[string]interface{}{
		{"isbn": "a", "title": "A"},
		{"isbn13": "b", "title": "B"},
	}
	m := indexBooksByISBN(books)
	if len(m) != 2 || m["a"]["title"] != "A" || m["b"]["title"] != "B" {
		t.Fatalf("%v", m)
	}
}

func TestLoadCollection_missingFileStartsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/new.json"
	m, err := loadCollection(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatal(m)
	}
	if err := saveCollection(path, map[string]map[string]interface{}{
		"x": {"isbn": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	raw := mustReadFile(t, path)
	if !strings.Contains(raw, `"x"`) {
		t.Fatalf("%s", raw)
	}
}
