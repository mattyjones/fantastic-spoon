package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const isbnConfigFileName = ".isbn_config.yml"

// AppConfig is CLI-level settings: lookup Config plus web server options.
type AppConfig struct {
	Config
	WebUI        bool
	Listen       string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// fileYAML mirrors .isbn_config.yml; pointers mean "omit = leave previous value".
type fileYAML struct {
	Input           *string `yaml:"input"`
	Output          *string `yaml:"output"`
	APIKey          *string `yaml:"api_key"`
	Batch           *int    `yaml:"batch"`
	APIURL          *string `yaml:"api_url"`
	RateEvery       *string `yaml:"rate_every"`
	Web             *bool   `yaml:"web"`
	Listen          *string `yaml:"listen"`
	ReadTimeout     *string `yaml:"read_timeout"`
	WriteTimeout    *string `yaml:"write_timeout"`
	IdleTimeout     *string `yaml:"idle_timeout"`
	Collection      *string `yaml:"collection"`
	StatusLog       *string `yaml:"status_log"`
	BookURLTemplate *string `yaml:"book_url_template"`
}

func findISBNConfigPath(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, isbnConfigFileName)
		st, err := os.Stat(candidate)
		if err == nil && !st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		Config: Config{
			OutputFile:      "results.json",
			BatchSize:       100,
			RateEvery:       time.Second,
			BookURLTemplate: DefaultBookURLTemplate,
		},
		Listen:       "127.0.0.1:8080",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func mergeAppConfig(startDir string) (AppConfig, error) {
	cfg := defaultAppConfig()

	path, err := findISBNConfigPath(startDir)
	if err != nil {
		return cfg, err
	}
	if path != "" {
		if err := applyYAMLFile(&cfg, path); err != nil {
			return cfg, err
		}
	}

	applyEnv(&cfg)
	return cfg, nil
}

func applyYAMLFile(cfg *AppConfig, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var y fileYAML
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	baseDir := filepath.Dir(path)
	if y.Input != nil {
		cfg.InputFile = resolveConfigPath(baseDir, *y.Input)
	}
	if y.Output != nil {
		cfg.OutputFile = resolveConfigPath(baseDir, *y.Output)
	}
	if y.APIKey != nil {
		cfg.APIKey = *y.APIKey
	}
	if y.Batch != nil {
		cfg.BatchSize = *y.Batch
	}
	if y.APIURL != nil {
		cfg.BooksURL = strings.TrimSpace(*y.APIURL)
	}
	if y.RateEvery != nil {
		d, err := time.ParseDuration(strings.TrimSpace(*y.RateEvery))
		if err != nil {
			return fmt.Errorf("%s: rate_every: %w", path, err)
		}
		cfg.RateEvery = d
	}
	if y.Web != nil {
		cfg.WebUI = *y.Web
	}
	if y.Listen != nil {
		cfg.Listen = strings.TrimSpace(*y.Listen)
	}
	if y.ReadTimeout != nil {
		d, err := time.ParseDuration(strings.TrimSpace(*y.ReadTimeout))
		if err != nil {
			return fmt.Errorf("%s: read_timeout: %w", path, err)
		}
		cfg.ReadTimeout = d
	}
	if y.WriteTimeout != nil {
		d, err := time.ParseDuration(strings.TrimSpace(*y.WriteTimeout))
		if err != nil {
			return fmt.Errorf("%s: write_timeout: %w", path, err)
		}
		cfg.WriteTimeout = d
	}
	if y.IdleTimeout != nil {
		d, err := time.ParseDuration(strings.TrimSpace(*y.IdleTimeout))
		if err != nil {
			return fmt.Errorf("%s: idle_timeout: %w", path, err)
		}
		cfg.IdleTimeout = d
	}
	if y.Collection != nil {
		cfg.CollectionFile = resolveConfigPath(baseDir, *y.Collection)
	}
	if y.StatusLog != nil {
		cfg.StatusLogFile = resolveConfigPath(baseDir, *y.StatusLog)
	}
	if y.BookURLTemplate != nil {
		cfg.BookURLTemplate = strings.TrimSpace(*y.BookURLTemplate)
	}
	return nil
}

func resolveConfigPath(baseDir, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}

func applyEnv(cfg *AppConfig) {
	if v := strings.TrimSpace(os.Getenv(EnvInputFile)); v != "" {
		cfg.InputFile = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvOutputFile)); v != "" {
		cfg.OutputFile = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvISBNAPKey)); v != "" {
		cfg.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvBatchSize)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.BatchSize = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvBooksURL)); v != "" {
		cfg.BooksURL = v
	} else if v := strings.TrimSpace(os.Getenv("ISBNDB_BOOKS_URL")); v != "" {
		cfg.BooksURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvRateEvery)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RateEvery = d
		}
	}
	if v, ok := parseBoolEnv(os.Getenv(EnvWeb)); ok {
		cfg.WebUI = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvListen)); v != "" {
		cfg.Listen = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvReadTimeout)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ReadTimeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvWriteTimeout)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.WriteTimeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvIdleTimeout)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.IdleTimeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvCollectionFile)); v != "" {
		cfg.CollectionFile = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvStatusLogFile)); v != "" {
		cfg.StatusLogFile = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvBookURLTemplate)); v != "" {
		cfg.BookURLTemplate = v
	}
}

func parseBoolEnv(s string) (val bool, ok bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return false, false
	}
	switch s {
	case "1", "true", "yes", "y", "on":
		return true, true
	case "0", "false", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func finalizeBooksURL(booksURL string) string {
	if strings.TrimSpace(booksURL) != "" {
		return strings.TrimSpace(booksURL)
	}
	if v := strings.TrimSpace(os.Getenv("ISBNDB_BOOKS_URL")); v != "" {
		return v
	}
	return "https://api2.isbndb.com/books"
}
