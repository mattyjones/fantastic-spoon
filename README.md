# fantastic-spoon

**fantastic-spoon** is a small command-line tool that reads a list of ISBNs from a text file, looks them up against a books HTTP API in configurable batches, and writes a single JSON document containing metadata and all returned book records.

It is aimed at workflows where you already have ISBNs (exports from a catalog, spreadsheets, or other tools) and need **bulk bibliographic metadata** without writing ad hoc scripts for pagination, pacing, and output shaping.

## What it is for

- Enriching or validating ISBN lists using an API that accepts **multi-ISBN** requests (the default contract matches **ISBNdb**’s books endpoint).
- Running **repeatable, batch-oriented** lookups with **client-side rate limiting** so you stay within provider limits.
- Producing **one JSON file** suitable for downstream processing, reporting, or import into other systems.

The program does not manage accounts, quotas, or API dashboards—it only performs the HTTP batch loop and file I/O.

## How it is designed

The pipeline is intentionally linear and easy to reason about:

1. **Input** — Read a file of newline-separated ISBNs (leading/trailing whitespace on the file is trimmed before splitting).
2. **Batching** — Split the list into chunks of size `-batch` (aligned with typical provider tier limits, e.g. 10 / 100 / 1000).
3. **Pacing** — Enforce a minimum interval between batch requests with a token bucket (`golang.org/x/time/rate`), so successive calls are spaced by at least `-rate-every` (default one second; zero or negative values are treated as one second).
4. **HTTP** — For each batch, send a **POST** with JSON body `{"isbns": "<comma-separated>"}` and `Authorization` set to your API key (ISBNdb-style: raw key string, not a `Bearer` prefix unless your provider expects that in the same header).
5. **Resilience** — If a batch fails (network error, non-200, rate limit), the error is **logged** and that batch is **skipped**; other batches still run. The final JSON reflects **only successfully retrieved** books.
6. **Output** — Marshal a document with `metadata` (e.g. `processed_at`, `total_found`) and a `books` array. Unknown fields inside each book object are preserved as generic JSON maps.

The **books URL** is resolved in this order: `-api-url` flag, then `ISBNDB_BOOKS_URL`, then the default `https://api2.isbndb.com/books`. That lets you point the same binary at a mock server for tests or another deployment without changing code.

## Requirements

- [Go](https://go.dev/dl/) **1.26.1** or compatible (see `go.mod`).
- A valid API key for your chosen books service (when using the default ISBNdb endpoint, an ISBNdb API key).

## Usage

### Environment variables

| Variable | Purpose |
|----------|---------|
| `ISBNDB_API_KEY` | Default API key if `-key` is omitted. |
| `ISBNDB_BOOKS_URL` | Override the books POST URL if `-api-url` is not set. |

### Command-line flags

| Flag | Default | Description |
|------|---------|-------------|
| `-input` | *(required)* | Path to a text file with **one ISBN per line**. |
| `-output` | `results.json` | Path for the written JSON file (mode `0644`). |
| `-key` | value of `ISBNDB_API_KEY` | API key sent in the `Authorization` header. |
| `-batch` | `100` | ISBNs per request (match your provider plan). |
| `-api-url` | see env / default | Full URL for the books **POST** endpoint. |
| `-rate-every` | `1s` | Minimum time between batch requests. |

### Examples

```bash
# Using an env var for the key (recommended so the key does not appear in shell history)
export ISBNDB_API_KEY="your-key"
go run . -input isbns.txt -output out.json

# Explicit key and batch size (e.g. Pro tier)
go run . -input isbns.txt -output out.json -key "$ISBNDB_API_KEY" -batch 1000 -rate-every 2s
```

After building (see below), run the binary the same way:

```bash
./bin/fantastic-spoon -input isbns.txt -output out.json
```

Progress messages are printed to standard output; batch errors are logged to the standard logger.

## Building and development

The [Makefile](Makefile) targets **Apple Silicon** (`darwin/arm64`) and writes `bin/fantastic-spoon`.

| Command | Description |
|---------|-------------|
| `make` / `make all` | Lint, test, verify modules, then build the binary. |
| `make lint` | `gofmt`, `go vet`, and `golangci-lint` (if installed). |
| `make test` | Run tests. |
| `make deps` | `go mod verify` and `go mod download`. |
| `make deps-update` | Upgrade module dependencies (`go get -u`) and `go mod tidy`. |
| `make build` | Build `bin/fantastic-spoon` for `darwin/arm64`. |
| `make clean` | Remove the `bin/` directory. |

To enable the repository’s Git **pre-commit** hook (tests, vet, fmt, etc.):

```bash
git config core.hooksPath .githooks
```

---

## License and security

**License.** This project is licensed under the **GNU General Public License v2.0**. The full text is in the [`LICENSE`](LICENSE) file in this repository.

**Security and privacy.**

- The API key is sent over **HTTPS** to the URL you configure (by default ISBNdb). Treat the key as a **secret**: prefer `ISBNDB_API_KEY` or your shell’s secret mechanism; avoid committing keys or putting them in shared logs.
- The tool **does not** hash or encrypt keys beyond what TLS provides; operational security (rotation, least privilege, monitoring) is your responsibility.
- You must comply with your **API provider’s terms of use**, quotas, and acceptable use. The program helps with pacing but does not guarantee you will never be rate-limited (`429` responses are treated as errors for that batch).
- Output JSON may contain **bibliographic or personal data** depending on what the API returns—handle files according to your policies.
- Software is provided **as-is**, without warranty of any kind; see the license for disclaimer of liability.

For security issues specific to this codebase (not your API account), please report them through the project’s normal contribution or maintainer channels.
