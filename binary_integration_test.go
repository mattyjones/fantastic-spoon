package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBinary_CLI(t *testing.T) {
	bin := buildTestBinary(t)
	srv, _ := newFakeISBNServer(t, fakeISBNServerOpts{APIKey: "k"})
	apiURL := srv.URL + "/books"
	rate := "1ns"

	t.Run("success_writes_output", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "isbns.txt")
		outPath := filepath.Join(dir, "results.json")
		mustWriteFile(t, inPath, "9780000000001\n9780000000002\n")

		cmd := exec.Command(bin,
			"-input", inPath,
			"-output", outPath,
			"-key", "k",
			"-batch", "1",
			"-api-url", apiURL,
			"-rate-every", rate,
		)
		cmd.Env = envWithoutISBNOverrides(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exit %v: %s", err, out)
		}
		if !strings.Contains(string(out), "Done! Saved 2 books") {
			t.Fatalf("stdout/stderr: %s", out)
		}

		got := mustReadJSONOutput(t, outPath)
		if got.Metadata.TotalFound != 2 || len(got.Books) != 2 {
			t.Fatalf("output: %+v", got)
		}
	})

	t.Run("collection_writes_status_log", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "isbns.txt")
		outPath := filepath.Join(dir, "results.json")
		collPath := filepath.Join(dir, "coll.json")
		logPath := filepath.Join(dir, "run.log")
		mustWriteFile(t, inPath, "9780000000001\n")

		cmd := exec.Command(bin,
			"-input", inPath,
			"-output", outPath,
			"-key", "k",
			"-batch", "1",
			"-api-url", apiURL,
			"-rate-every", rate,
			"-collection", collPath,
			"-status-log", logPath,
		)
		cmd.Env = envWithoutISBNOverrides(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exit %v: %s", err, out)
		}
		if !strings.Contains(string(out), "Collection updated") {
			t.Fatalf("stdout/stderr: %s", out)
		}
		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "9780000000001\tadded\tsuccessful\t") {
			t.Fatalf("log: %s", raw)
		}
	})

	t.Run("env_ISBNDB_BOOKS_URL_used_when_api_url_empty", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "in.txt")
		outPath := filepath.Join(dir, "out.json")
		mustWriteFile(t, inPath, "one\n")

		cmd := exec.Command(bin,
			"-input", inPath,
			"-output", outPath,
			"-key", "k",
			"-batch", "10",
			"-rate-every", rate,
		)
		env := envWithoutISBNOverrides(t)
		env = append(env, "ISBNDB_BOOKS_URL="+apiURL)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exit %v: %s", err, out)
		}

		got := mustReadJSONOutput(t, outPath)
		if got.Metadata.TotalFound != 1 {
			t.Fatalf("total_found = %d", got.Metadata.TotalFound)
		}
	})

	t.Run("exit_error_when_input_missing", func(t *testing.T) {
		cmd := exec.Command(bin, "-key", "k", "-api-url", apiURL, "-rate-every", rate)
		cmd.Env = envWithoutISBNOverrides(t)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit, output: %s", out)
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("err = %v, output: %s", err, out)
		}
		if !strings.Contains(string(out), "required") {
			t.Fatalf("output: %s", out)
		}
	})

	t.Run("exit_error_when_key_missing", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "in.txt")
		mustWriteFile(t, inPath, "x\n")

		cmd := exec.Command(bin, "-input", inPath, "-api-url", apiURL, "-rate-every", rate)
		cmd.Env = envWithoutISBNOverrides(t)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit, output: %s", out)
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("err = %v, output: %s", err, out)
		}
		if !strings.Contains(string(out), "required") {
			t.Fatalf("output: %s", out)
		}
	})

	t.Run("exit_error_when_input_file_unreadable", func(t *testing.T) {
		dir := t.TempDir()
		outPath := filepath.Join(dir, "out.json")
		missing := filepath.Join(dir, "missing.txt")

		cmd := exec.Command(bin,
			"-input", missing,
			"-output", outPath,
			"-key", "k",
			"-api-url", apiURL,
			"-rate-every", rate,
		)
		cmd.Env = envWithoutISBNOverrides(t)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit, output: %s", out)
		}
		if !strings.Contains(string(out), "read input file") {
			t.Fatalf("output: %s", out)
		}
	})

	t.Run("yaml_config_in_working_dir", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "isbns.txt")
		outPath := filepath.Join(dir, "results.json")
		mustWriteFile(t, inPath, "9780000000001\n9780000000002\n")
		yaml := fmt.Sprintf(`input: isbns.txt
output: results.json
api_key: k
batch: 1
api_url: %s
rate_every: 1ns
web: false
`, apiURL)
		if err := os.WriteFile(filepath.Join(dir, isbnConfigFileName), []byte(yaml), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin)
		cmd.Dir = dir
		cmd.Env = envWithoutISBNOverrides(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exit %v: %s", err, out)
		}
		if !strings.Contains(string(out), "Done! Saved 2 books") {
			t.Fatalf("stdout/stderr: %s", out)
		}
		got := mustReadJSONOutput(t, outPath)
		if got.Metadata.TotalFound != 2 || len(got.Books) != 2 {
			t.Fatalf("output: %+v", got)
		}
	})

	t.Run("env_vars_without_yaml", func(t *testing.T) {
		dir := t.TempDir()
		inPath := filepath.Join(dir, "in.txt")
		outPath := filepath.Join(dir, "out.json")
		mustWriteFile(t, inPath, "one\n")
		cmd := exec.Command(bin)
		cmd.Dir = dir
		env := envWithoutISBNOverrides(t)
		env = append(env,
			EnvInputFile+"="+inPath,
			EnvOutputFile+"="+outPath,
			EnvISBNAPKey+"=k",
			EnvBatchSize+"=10",
			"ISBNDB_BOOKS_URL="+apiURL,
			EnvRateEvery+"=1ns",
		)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exit %v: %s", err, out)
		}
		got := mustReadJSONOutput(t, outPath)
		if got.Metadata.TotalFound != 1 {
			t.Fatalf("total_found = %d", got.Metadata.TotalFound)
		}
	})
}

func buildTestBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "isbn_lookup_cli_test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", path, ".")
	cmd.Dir = testModuleRoot(t)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, raw)
	}
	return path
}

func testModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func envWithoutISBNOverrides(t *testing.T) []string {
	t.Helper()
	skip := map[string]struct{}{
		EnvISBNAPKey:       {},
		"ISBNDB_BOOKS_URL": {},
		EnvBooksURL:        {},
	}
	var out []string
	for _, e := range os.Environ() {
		i := strings.IndexByte(e, '=')
		if i <= 0 {
			continue
		}
		key := e[:i]
		if _, ok := skip[key]; ok {
			continue
		}
		if strings.HasPrefix(key, "FANTASTIC_SPOON_") {
			continue
		}
		out = append(out, e)
	}
	return out
}
