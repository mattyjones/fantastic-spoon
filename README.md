# fantastic-spoon

**fantastic-spoon** reads newline-separated ISBNs, queries a books HTTP API in configurable batches with client-side rate limiting, and returns aggregated JSON (metadata plus book records). You can run it as a **CLI** (read ISBNs from a file and write a JSON file) or as a **local web app** (paste ISBNs or load a text file in the browser).

It is aimed at workflows where you already have ISBNs (exports from a catalog, spreadsheets, or other tools) and need **bulk bibliographic metadata** without writing ad hoc scripts for pagination, pacing, and output shaping.

## Requirement terminology

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

## What it is for

- Enriching or validating ISBN lists using an API that accepts **multi-ISBN** requests (the default contract matches **ISBNdb**’s books endpoint).
- Running **repeatable, batch-oriented** lookups with **client-side rate limiting** so you stay within provider limits.
- Producing **JSON** suitable for downstream processing, reporting, or import into other systems (written to disk from the CLI, or returned in the browser from the web UI).

The program does not manage accounts, quotas, or API dashboards—it only performs the HTTP batch loop and I/O you configure.

## How it is designed

### Pipeline (CLI and web)

The core behavior is the same for both interfaces:

1. **Input** — One ISBN per line (leading/trailing whitespace on the whole blob is trimmed before splitting).
2. **Batching** — Split the list into chunks of size `-batch` / **Batch size** (SHOULD match your provider tier limits, e.g. 10 / 100 / 1000).
3. **Pacing** — Enforce a minimum interval between batch requests with a token bucket (`golang.org/x/time/rate`), so successive calls are spaced by at least `-rate-every` / **Seconds between batches** (default one second; zero or negative values MUST be treated as one second in the implementation).
4. **HTTP** — For each batch, send a **POST** with JSON body `{"isbns": "<comma-separated>"}` and `Authorization` set to your API key (ISBNdb-style: raw key string, not a `Bearer` prefix unless your provider expects that in the same header).
5. **Resilience** — If a batch fails (network error, non-200, rate limit), the error is **logged** and that batch is **skipped**; other batches still run. The final JSON reflects **only successfully retrieved** books.
6. **Output** — A document with `metadata` (e.g. `processed_at`, `total_found`) and a `books` array. Unknown fields inside each book object are preserved as generic JSON maps.

### Collection mode (OPTIONAL)

When **`-collection`** / **`collection_file`** is set:

1. The tool loads a JSON **collection** file (`version` + `books` map keyed by normalized ISBN). Missing file starts empty.
2. After each successful batch, for **every input ISBN** in that batch it decides:
   - **`added`** / **successful** — API returned a book and the ISBN was not already in the collection; the record is stored and a **URL** is recorded in the log (from the API `url` / `link` / … field if present, else **`book-url-template`** with `%s` = ISBN).
   - **`duplicate`** / **successful** — API returned a book and the ISBN was already in the collection; the log line still includes the **URL**.
   - **`not_added`** / **unsuccessful** — Batch failed, API returned no row for that ISBN, or the line was empty; **no URL** on the log line.
3. When all batches finish, the collection file is rewritten and a **text status log** is written: **one line per input ISBN** (in input order), tab-separated:

   `ISBN<TAB>added|duplicate|not_added<TAB>successful|unsuccessful<TAB>URL`

   The URL field is empty when status is `not_added`.

4. Result JSON `metadata` MAY include `collection_added`, `collection_duplicates`, `collection_not_added`, and `status_log_path`.

### Configuration sources (YAML, environment, CLI)

Settings are merged in this order (later steps override earlier ones):

1. **Built-in defaults** (for example `results.json`, batch size `100`, `rate_every` one second, localhost web listen address, and web server timeouts).
2. **`.isbn_config.yml`** — If this file exists, the program searches upward from the **current working directory** (the directory you pass to `os.Getwd()` when the process starts) through parent directories until it finds **`.isbn_config.yml`**, then loads it. If the file is **not** found anywhere on that path, configuration from YAML is skipped entirely (no error).
3. **Environment variables** — Each variable that is set MUST override the corresponding value from the file (see tables below).
4. **CLI flags** — Each flag that is passed MUST override the merged value from file and environment for that run.

Relative paths in the YAML file (`input`, `output`, `collection`, `status_log`) are resolved against the **directory that contains** `.isbn_config.yml`, not necessarily the process working directory.

**Example `.isbn_config.yml`**

```yaml
# Path to the newline-separated ISBN list (relative to this file’s directory)
input: data/isbn_list.txt
output: out/results.json
api_key: your-api-key   # OPTIONAL here; you SHOULD prefer ISBN_API_KEY in the environment
batch: 100
api_url: https://api2.isbndb.com/books
rate_every: 1s
web: false
listen: 127.0.0.1:8080
read_timeout: 15s
write_timeout: 30s
idle_timeout: 60s
collection: ""
status_log: ""
book_url_template: https://isbndb.com/book/%s
```

Omit keys you do not need; boolean **`web`** defaults to `false` when absent. You SHOULD use **`ISBN_API_KEY`** instead of **`api_key`** in the file when sharing examples or version control, so secrets are not written to disk.

### API key configuration

- **CLI** — Pass **`-key`**, set **`api_key`** in `.isbn_config.yml`, and/or set **`ISBN_API_KEY`** (see `EnvISBNAPIKey` in `env.go`). When both are set, **`ISBN_API_KEY`** MUST override a key from YAML.
- **Web UI** — The browser MUST NOT send or receive the API key. Only the server process MAY read **`ISBN_API_KEY`** from the environment when you start `go run . -web` (or your built binary). There MUST NOT be a key field in the HTML or in the JSON request body. Batch size, rate, collection paths, and **`book_url_template`** still default from `.isbn_config.yml` and **`FANTASTIC_SPOON_*`** variables when the JSON request omits them. The web handler uses the server-configured books URL only (request JSON MUST NOT override it).

### Code layout

| Piece | Role |
|-------|------|
| `env.go` | Declares `EnvISBNAPIKey` and `FANTASTIC_SPOON_*` names for env-based overrides. |
| `config.go` | Discovers `.isbn_config.yml`, parses YAML, merges env into `AppConfig`, resolves `finalizeBooksURL`. |
| `main()` | Merges file + env, registers flags (defaults reflect that merge), parses flags, then runs the CLI or web server. |
| `run()` | CLI: reads `InputFile`, calls **`runLookup`**, writes **`OutputFile`**, prints a completion line to stdout. |
| **`runLookup()`** | Shared implementation: batching, rate limiting, HTTP calls, and the in-memory result map used by both CLI and web. |
| `collection.go` | Load/save collection JSON, per-ISBN status lines, URL resolution from API or template. |
| `web.go` | Local HTTP server (`GET /`, `POST /api/lookup`), embeds the `web/` static assets via `embed.FS`. |
| `web/index.html` | Single-page UI (ISBNs, file picker, OPTIONAL collection paths on the server). |

The **books URL** after merging config is: use the non-empty value from flags / YAML / env (`FANTASTIC_SPOON_API_URL` or legacy **`ISBNDB_BOOKS_URL`**), then default to `https://api2.isbndb.com/books`. The web handler always uses this server-side value; request JSON cannot override it.

### Policy tests and hooks

- **`policy_secrets_test.go`** — When you run `go test`, tracked files are scanned (via `git ls-files`) for suspicious **`ISBN_API_KEY=`** / legacy **`ISBNDB_API_KEY=`** assignments and for committed **`.env`** / **`.env.local`** files. Keep real keys out of the repository.
- **`.githooks/pre-commit`** — Before commit, staged content is scanned for common token patterns, PEM blocks, long `ISBN_API_KEY=` / `ISBNDB_API_KEY=` lines, and disallowed env filenames (see the hook script for details).

## Requirements

- [Go](https://go.dev/dl/) **1.26.1** or newer (`go` directive in `go.mod`); that is the minimum toolchain version this module is written to compile against. Development builds and `go test` are run on **darwin/arm64** (macOS, Apple Silicon).
- A valid API key for your chosen books service (when using the default ISBNdb endpoint, an ISBNdb API key).

## Quickstart

Use this when you just want to run it now.

### Web interface (local)

```bash
export ISBN_API_KEY="your-key"
go run . -web
# Open http://127.0.0.1:8080
```

Paste ISBNs (one per line) or upload a text file, then click **Look up**.

### CLI with the included `book_list`

```bash
export ISBN_API_KEY="your-key"
go run . -input book_list -output results.json
```

The JSON result is written to `results.json`.

## Usage

### Environment variables

| Variable | Purpose |
|----------|---------|
| **`ISBN_API_KEY`** | Books API credential. Merged from env after YAML; default for **`-key`**; **REQUIRED** in the environment for the **web** server (MUST NOT be entered in the browser). |
| **`ISBNDB_BOOKS_URL`** | Legacy alias for the books POST URL (same tier as **`FANTASTIC_SPOON_API_URL`**). Ignored if **`FANTASTIC_SPOON_API_URL`** is set. |
| **`FANTASTIC_SPOON_INPUT`** | CLI **`-input`** path. |
| **`FANTASTIC_SPOON_OUTPUT`** | CLI **`-output`** path. |
| **`FANTASTIC_SPOON_BATCH`** | Integer **`-batch`** size. |
| **`FANTASTIC_SPOON_API_URL`** | Books POST URL (**`-api-url`**). |
| **`FANTASTIC_SPOON_RATE_EVERY`** | Duration string for **`-rate-every`** (for example `1s`, `500ms`). |
| **`FANTASTIC_SPOON_WEB`** | `true` / `false` / `1` / `0` for **`-web`**. |
| **`FANTASTIC_SPOON_LISTEN`** | **`-listen`** address. |
| **`FANTASTIC_SPOON_READ_TIMEOUT`** | Duration string for **`-read-timeout`** (for example `15s`). |
| **`FANTASTIC_SPOON_WRITE_TIMEOUT`** | Duration string for **`-write-timeout`** (for example `30s`). |
| **`FANTASTIC_SPOON_IDLE_TIMEOUT`** | Duration string for **`-idle-timeout`** (for example `60s`). |
| **`FANTASTIC_SPOON_COLLECTION`** | **`-collection`** path. |
| **`FANTASTIC_SPOON_STATUS_LOG`** | **`-status-log`** path. |
| **`FANTASTIC_SPOON_BOOK_URL_TEMPLATE`** | **`-book-url-template`**. |

### Command-line flags

Flag **defaults** reflect `.isbn_config.yml` (if found) plus the environment; a flag that is passed MUST override that merged value.

| Flag | Default | Description |
|------|---------|-------------|
| `-input` | merged | Path to a text file with **one ISBN per line**. REQUIRED for CLI unless set via YAML/env. Not used with `-web`. |
| `-output` | merged (`results.json`) | Path for the written JSON file (mode `0644`). |
| `-key` | merged | API key sent in the `Authorization` header. |
| `-batch` | merged (`100`) | ISBNs per request (match your provider plan). |
| `-api-url` | merged | Full URL for the books **POST** endpoint. |
| `-rate-every` | merged (`1s`) | Minimum time between batch requests. |
| `-web` | merged (`false`) | Start a **local web UI** instead of the CLI. |
| `-listen` | merged (`127.0.0.1:8080`) | Listen address for `-web`. MUST be loopback (`127.0.0.1`, `::1`, or `localhost`) in strict localhost mode. |
| `-read-timeout` | merged (`15s`) | Max duration to read the full request in web mode. |
| `-write-timeout` | merged (`30s`) | Max duration to write a response in web mode. |
| `-idle-timeout` | merged (`60s`) | Keep-alive timeout for idle web connections in web mode. |
| `-collection` | merged | JSON file to merge unique books into (enables status log). |
| `-status-log` | merged | Text log path (`<output-basename>.status.log` or `<collection-basename>.status.log`). |
| `-book-url-template` | merged | `Printf` template when the API record has no URL (exactly one `%s` for ISBN). |

### Web UI (local)

Set the key **only** in the shell environment before starting the server:

```bash
export ISBN_API_KEY="your-key"
go run . -web
# Open http://127.0.0.1:8080 — paste ISBNs (one per line) or choose a text file, then **Look up**.
```

- **GET /** — Serves the HTML UI (embedded from `web/index.html`).
- **POST /api/lookup** — JSON body (max ~2 MiB), JSON response. The server MUST NOT use an API key from the JSON body for outbound API calls; extra fields such as `api_key` are ignored by the decoder and MUST NOT override `ISBN_API_KEY`.

**Request body (JSON)**

| Field | Type | Description |
|-------|------|-------------|
| `isbns` | string | Newline-separated ISBNs (same rules as a CLI input file). |
| `batch_size` | number | If missing or ≤ 0, uses the server default from `.isbn_config.yml` / **`FANTASTIC_SPOON_BATCH`**, then `100`. |
| `rate_every_sec` | number | Whole seconds between batches. If missing or ≤ 0, uses the server default **`rate_every`** (from YAML / **`FANTASTIC_SPOON_RATE_EVERY`**), then `1s`. |
| `collection_file` | string | OPTIONAL. Same as **`-collection`**; if empty, uses server default from YAML / **`FANTASTIC_SPOON_COLLECTION`**. |
| `status_log_file` | string | OPTIONAL. Same as **`-status-log`**; if empty, uses server default from YAML / **`FANTASTIC_SPOON_STATUS_LOG`**. |
| `book_url_template` | string | OPTIONAL. Same as **`-book-url-template`**; if empty, uses server default from YAML / **`FANTASTIC_SPOON_BOOK_URL_TEMPLATE`**. |

**Successful response (JSON)**

| Field | Description |
|-------|-------------|
| `data` | Same object as the CLI output file: `metadata` + `books`. |
| `log` | Text progress lines that would otherwise go to stdout during a CLI run. |

**Error response (JSON)** — HTTP 4xx with `{ "error": "<message>", "log": "<partial progress if any>" }`.

The server binds to **127.0.0.1** by default so it is not exposed on your LAN. In strict localhost mode, non-loopback listen addresses are rejected.

### Examples (CLI)

```bash
# RECOMMENDED: put paths, batching, and URL in .isbn_config.yml at the repo root, then only set the key in the environment
export ISBN_API_KEY="your-key"
go run .

# Or pass everything on the command line
export ISBN_API_KEY="your-key"
go run . -input isbns.txt -output out.json

# Env-only (no YAML): set FANTASTIC_SPOON_* and ISBN_API_KEY, then run the binary with no I/O flags
export ISBN_API_KEY="your-key"
export FANTASTIC_SPOON_INPUT=isbns.txt
export FANTASTIC_SPOON_OUTPUT=out.json
go run .

# Explicit key and batch size (e.g. Pro tier)
go run . -input isbns.txt -output out.json -key "$ISBN_API_KEY" -batch 1000 -rate-every 2s

# Merge into a collection and write per-ISBN status log (paths on your machine)
go run . -input isbns.txt -output out.json -collection ~/books/collection.json -status-log ~/books/last-run.status.log
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
| `make test` | Run tests (including web handlers, `runLookup`, and **policy** scans for env secrets). |
| `make deps` | `go mod verify` and `go mod download`. |
| `make deps-update` | Upgrade module dependencies (`go get -u`) and `go mod tidy`. |
| `make build` | Build `bin/fantastic-spoon` for `darwin/arm64`. |
| `make clean` | Remove the `bin/` directory. |

To enable the repository’s Git **pre-commit** hook (tests, vet, fmt, secret patterns, etc.):

```bash
git config core.hooksPath .githooks
```

---

## License and security

**License.** This project is licensed under the **GNU General Public License v2.0**. The full text is in the [`LICENSE`](LICENSE) file in this repository.

**Security and privacy.**

- The API key is sent over **HTTPS** to the URL you configure (by default ISBNdb). Treat the key as a **secret**: use **`ISBN_API_KEY`** in the environment or **`-key`** on the CLI; you MUST NOT commit keys, real `.env` files, or **`.isbn_config.yml`** files that embed **`api_key`**, or long assignments in documentation.
- The **web UI MUST NOT collect the API key**; configure **`ISBN_API_KEY` only on the server process** before starting. The server is intended for strict localhost use and rejects non-loopback listen addresses.
- The tool **does not** hash or encrypt keys beyond what TLS provides; operational security (rotation, least privilege, monitoring) is your responsibility.
- You MUST comply with your **API provider’s terms of use**, quotas, and acceptable use. The program helps with pacing but does not guarantee you will never be rate-limited (`429` responses are treated as errors for that batch).
- Output JSON MAY contain **bibliographic or personal data** depending on what the API returns—handle files and browser results according to your policies.
- Software is provided **as-is**, without warranty of any kind; see the license for disclaimer of liability.

For security issues specific to this codebase (not your API account), please report them through the project’s normal contribution or maintainer channels.
