package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Disallow committing env-var assignments that look like real API keys (length),
// and the legacy ISBNDB_API_KEY name with values. Short documentation placeholders are allowed.
var (
	reISBNAPIKeyAssign   = regexp.MustCompile(`ISBN_API_KEY\s*=\s*['\"]?([^\s#'"\n]+)`)
	reISBNDBAPIKeyAssign = regexp.MustCompile(`ISBNDB_API_KEY\s*=\s*['\"]?([^\s#'"\n]+)`)
)

func allowedEnvPlaceholder(val string) bool {
	v := strings.Trim(strings.TrimSpace(val), `"'`)
	switch strings.ToLower(v) {
	case "", "your-key", "changeme", "xxx", "secret", "key", "k":
		return true
	}
	if len(v) < 16 {
		return true
	}
	return false
}

func lineViolatesEnvPolicy(line string) (reason string) {
	line = strings.TrimSpace(line)
	if strings.Contains(line, `Getenv("ISBN_API_KEY")`) || strings.Contains(line, `Getenv(EnvISBNAPIKey)`) {
		return ""
	}
	if m := reISBNAPIKeyAssign.FindStringSubmatch(line); len(m) == 2 {
		if !allowedEnvPlaceholder(m[1]) {
			return "ISBN_API_KEY assignment looks like a real secret (use a short placeholder in docs, or rely on env at runtime)"
		}
	}
	if m := reISBNDBAPIKeyAssign.FindStringSubmatch(line); len(m) == 2 {
		if !allowedEnvPlaceholder(m[1]) {
			return "ISBNDB_API_KEY assignment (legacy name) looks like a real secret"
		}
	}
	return ""
}

func TestTrackedFilesNoSuspiciousAPIKeyAssignments(t *testing.T) {
	root, err := moduleRootDir(t)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skip("git ls-files failed (not a git checkout?): ", err)
	}
	for _, path := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if path == "" {
			continue
		}
		base := filepath.Base(path)
		switch base {
		case ".env", ".env.local", ".env.production", ".env.development":
			t.Errorf("%s: do not commit environment files with secrets; use .env.example and document ISBN_API_KEY in README", path)
			continue
		}
		switch filepath.Ext(path) {
		case ".go", ".md", ".html", ".yml", ".yaml", ".json", ".sh", ".toml", "":
			// continue
		default:
			continue
		}
		if strings.HasPrefix(path, "vendor/") {
			continue
		}
		full := filepath.Join(root, path)
		file, err := os.Open(full)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		sc := bufio.NewScanner(file)
		const maxLine = 256 * 1024
		sc.Buffer(make([]byte, 0, 4096), maxLine)
		n := 0
		for sc.Scan() {
			n++
			if r := lineViolatesEnvPolicy(sc.Text()); r != "" {
				t.Errorf("%s:%d: %s", path, n, r)
			}
		}
		if err := sc.Err(); err != nil {
			_ = file.Close()
			t.Fatalf("%s: %v", path, err)
		}
		_ = file.Close()
	}
}

func moduleRootDir(t *testing.T) (string, error) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return wd, nil
}
