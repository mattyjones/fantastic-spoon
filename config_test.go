package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindISBNConfigPath_walksUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, isbnConfigFileName)
	if err := os.WriteFile(cfgPath, []byte("input: in.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	found, err := findISBNConfigPath(sub)
	if err != nil {
		t.Fatal(err)
	}
	if found != cfgPath {
		t.Fatalf("got %q want %q", found, cfgPath)
	}
}

func TestFindISBNConfigPath_notFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	found, err := findISBNConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if found != "" {
		t.Fatalf("got %q want empty", found)
	}
}

func TestMergeAppConfig_yamlRelativePaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "data")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, isbnConfigFileName)
	content := "input: data/isbn.txt\noutput: out/result.json\nbatch: 5\n"
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	app, err := mergeAppConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantIn := filepath.Join(dir, "data", "isbn.txt")
	if app.InputFile != wantIn {
		t.Fatalf("input = %q want %q", app.InputFile, wantIn)
	}
	wantOut := filepath.Join(dir, "out", "result.json")
	if app.OutputFile != wantOut {
		t.Fatalf("output = %q want %q", app.OutputFile, wantOut)
	}
	if app.BatchSize != 5 {
		t.Fatalf("batch = %d", app.BatchSize)
	}
}

func TestMergeAppConfig_envOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := filepath.Join(dir, isbnConfigFileName)
	if err := os.WriteFile(yaml, []byte("input: a.txt\nbatch: 9\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvInputFile, "/env/in.txt")
	t.Setenv(EnvBatchSize, "3")
	app, err := mergeAppConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if app.InputFile != "/env/in.txt" {
		t.Fatalf("input = %q", app.InputFile)
	}
	if app.BatchSize != 3 {
		t.Fatalf("batch = %d", app.BatchSize)
	}
}

func TestMergeAppConfig_noYAMLUsesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvBatchSize, "7")
	app, err := mergeAppConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if app.BatchSize != 7 {
		t.Fatalf("batch = %d", app.BatchSize)
	}
	if app.OutputFile != "results.json" {
		t.Fatalf("output = %q", app.OutputFile)
	}
}

func TestFinalizeBooksURL(t *testing.T) {
	t.Setenv("ISBNDB_BOOKS_URL", "")
	if got := finalizeBooksURL(""); got != "https://api2.isbndb.com/books" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("ISBNDB_BOOKS_URL", "https://env.example/books")
	if got := finalizeBooksURL(""); got != "https://env.example/books" {
		t.Fatalf("got %q", got)
	}
	if got := finalizeBooksURL("  https://example/books  "); got != "https://example/books" {
		t.Fatalf("got %q", got)
	}
}
