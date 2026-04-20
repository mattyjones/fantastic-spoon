package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
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

func startWebUI(listenAddr string, defaults Config, readTimeout, writeTimeout, idleTimeout time.Duration) error {
	if err := validateLoopbackListenAddr(listenAddr); err != nil {
		return err
	}
	h := &webHandler{defaults: defaults}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebIndex)
	mux.HandleFunc("/api/lookup", h.handleWebLookup)
	if readTimeout <= 0 {
		readTimeout = 15 * time.Second
	}
	if writeTimeout <= 0 {
		writeTimeout = 30 * time.Second
	}
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
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
	if !webRequestOriginAllowed(r) {
		writeWebError(w, http.StatusForbidden, "forbidden origin; localhost web endpoint only accepts same-origin requests", "")
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

	booksURL := finalizeBooksURL(h.defaults.BooksURL)

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

func validateLoopbackListenAddr(listenAddr string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", listenAddr, err)
	}
	if host == "" {
		return fmt.Errorf("listen host must be explicit loopback (e.g. 127.0.0.1 or localhost), got %q", listenAddr)
	}
	if isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("listen host %q is not loopback; strict localhost mode only allows 127.0.0.1, ::1, or localhost", host)
}

func webRequestOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.ParseRequestURI(origin)
	if err != nil {
		return false
	}
	if !isLoopbackHost(u.Hostname()) {
		return false
	}

	reqHost, reqPort := splitHostPortLoose(strings.TrimSpace(r.Host))
	if reqHost == "" || !isLoopbackHost(reqHost) {
		return false
	}
	originPort := normalizePort(u.Port(), u.Scheme)
	requestPort := normalizePort(reqPort, "http")
	return originPort != "" && originPort == requestPort
}

func splitHostPortLoose(hostport string) (string, string) {
	if hostport == "" {
		return "", ""
	}
	host, port, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.TrimSpace(host), strings.TrimSpace(port)
	}
	return strings.TrimSpace(hostport), ""
}

func normalizePort(port, scheme string) string {
	port = strings.TrimSpace(port)
	if port != "" {
		return port
	}
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "443"
	default:
		return "80"
	}
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
