package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web
var webStatic embed.FS

const webMaxBodyBytes = 2 << 20 // 2 MiB

type webHandler struct {
	defaults Config
}

func startWebUI(listenAddr string, defaults Config) error {
	h := &webHandler{defaults: defaults}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebIndex)
	mux.HandleFunc("/api/lookup", h.handleWebLookup)
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("fantastic-spoon web UI: http://%s", listenAddr)
	return srv.ListenAndServe()
}

func handleWebIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, err := webStatic.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "web UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

type webLookupRequest struct {
	ISBNs           string `json:"isbns"`
	BatchSize       int    `json:"batch_size"`
	RateEverySec    int    `json:"rate_every_sec"`
	APIURL          string `json:"api_url"`
	CollectionFile  string `json:"collection_file"`
	StatusLogFile   string `json:"status_log_file"`
	BookURLTemplate string `json:"book_url_template"`
}

type webLookupResponse struct {
	Data map[string]interface{} `json:"data"`
	Log  string                 `json:"log"`
}

func (h *webHandler) handleWebLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, webMaxBodyBytes)

	var req webLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWebError(w, http.StatusBadRequest, "invalid JSON body", "")
		return
	}

	apiKey := strings.TrimSpace(os.Getenv(EnvISBNAPKey))
	if apiKey == "" {
		writeWebError(w, http.StatusBadRequest, "API key MUST be set: set environment variable "+EnvISBNAPKey+" for the server process (the server MUST NOT read the key from the JSON body)", "")
		return
	}

	raw := strings.TrimSpace(req.ISBNs)
	if raw == "" {
		writeWebError(w, http.StatusBadRequest, "Enter at least one ISBN or load a file", "")
		return
	}

	batch := req.BatchSize
	if batch <= 0 {
		batch = h.defaults.BatchSize
	}
	if batch <= 0 {
		batch = 100
	}

	rateDur := time.Duration(req.RateEverySec) * time.Second
	if req.RateEverySec <= 0 {
		rateDur = h.defaults.RateEvery
	}
	if rateDur <= 0 {
		rateDur = time.Second
	}

	booksURL := strings.TrimSpace(req.APIURL)
	if booksURL == "" {
		booksURL = finalizeBooksURL(h.defaults.BooksURL)
	}

	collection := strings.TrimSpace(req.CollectionFile)
	if collection == "" {
		collection = h.defaults.CollectionFile
	}
	statusLog := strings.TrimSpace(req.StatusLogFile)
	if statusLog == "" {
		statusLog = h.defaults.StatusLogFile
	}
	tpl := strings.TrimSpace(req.BookURLTemplate)
	if tpl == "" {
		tpl = h.defaults.BookURLTemplate
	}
	if tpl == "" {
		tpl = DefaultBookURLTemplate
	}

	cfg := Config{
		APIKey:          apiKey,
		BatchSize:       batch,
		BooksURL:        booksURL,
		RateEvery:       rateDur,
		CollectionFile:  collection,
		StatusLogFile:   statusLog,
		BookURLTemplate: tpl,
	}

	allISBNs := strings.Split(strings.TrimSpace(raw), "\n")

	var buf bytes.Buffer
	ctx := r.Context()
	out, err := runLookup(ctx, cfg, allISBNs, &buf)
	if err != nil {
		writeWebError(w, http.StatusBadRequest, err.Error(), buf.String())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(webLookupResponse{Data: out, Log: buf.String()})
}

func writeWebError(w http.ResponseWriter, status int, message, logText string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
		"log":   logText,
	})
}
