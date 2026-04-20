package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CollectionFileJSON is the on-disk format for the book collection.
type CollectionFileJSON struct {
	Version int                               `json:"version"`
	Books   map[string]map[string]interface{} `json:"books"`
}

const collectionFormatVersion = 1

// DefaultBookURLTemplate is used when the API record has no URL field (printf-style, one %s for ISBN).
const DefaultBookURLTemplate = "https://isbndb.com/book/%s"

func normalizeISBN(s string) string {
	return strings.TrimSpace(s)
}

func bookISBN(b map[string]interface{}) string {
	for _, k := range []string{"isbn", "isbn13", "isbn10"} {
		if v, ok := b[k]; ok {
			switch x := v.(type) {
			case string:
				if s := normalizeISBN(x); s != "" {
					return s
				}
			case float64:
				return normalizeISBN(fmt.Sprintf("%.0f", x))
			}
		}
	}
	return ""
}

func bookURL(b map[string]interface{}, isbn, template string) string {
	if template == "" {
		template = DefaultBookURLTemplate
	}
	for _, k := range []string{"url", "link", "book_url", "href"} {
		if v, ok := b[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return fmt.Sprintf(template, isbn)
}

func loadCollection(path string) (map[string]map[string]interface{}, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[string]interface{}), nil
		}
		return nil, fmt.Errorf("read collection: %w", err)
	}
	var doc CollectionFileJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode collection: %w", err)
	}
	if doc.Books == nil {
		doc.Books = make(map[string]map[string]interface{})
	}
	return doc.Books, nil
}

func saveCollection(path string, books map[string]map[string]interface{}) error {
	doc := CollectionFileJSON{
		Version: collectionFormatVersion,
		Books:   books,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

// StatusLineKind for the text status log.
type StatusLineKind string

const (
	StatusAdded        StatusLineKind = "added"
	StatusNotAdded     StatusLineKind = "not_added"
	StatusDuplicate    StatusLineKind = "duplicate"
	StatusSuccessful   string         = "successful"
	StatusUnsuccessful string         = "unsuccessful"
)

// formatStatusLogLine: ISBN, kind (added|not_added|duplicate), outcome (successful|unsuccessful), URL (empty if not_added).
func formatStatusLogLine(isbn string, kind StatusLineKind, url string) string {
	isbn = normalizeISBN(isbn)
	switch kind {
	case StatusAdded, StatusDuplicate:
		return fmt.Sprintf("%s\t%s\t%s\t%s", isbn, kind, StatusSuccessful, url)
	case StatusNotAdded:
		return fmt.Sprintf("%s\t%s\t%s\t", isbn, kind, StatusUnsuccessful)
	default:
		return fmt.Sprintf("%s\t%s\t%s\t", isbn, StatusNotAdded, StatusUnsuccessful)
	}
}

func writeStatusLog(path string, lines []string) error {
	if path == "" {
		return nil
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func defaultStatusLogPath(outputJSONPath string) string {
	if outputJSONPath == "" {
		return "status.log"
	}
	base := outputJSONPath
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	return base + ".status.log"
}

func defaultStatusLogFromCollection(collectionPath string) string {
	if collectionPath == "" {
		return "status.log"
	}
	base := collectionPath
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	return base + ".status.log"
}

// indexBooksByISBN maps normalized ISBN from API records to the record.
func indexBooksByISBN(books []map[string]interface{}) map[string]map[string]interface{} {
	out := make(map[string]map[string]interface{}, len(books))
	for _, b := range books {
		id := bookISBN(b)
		if id != "" {
			out[normalizeISBN(id)] = b
		}
	}
	return out
}

func cloneBookMap(b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}
