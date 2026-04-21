package main

// EnvISBNAPIKey is the environment variable name for the books API credential.
// The CLI reads it as the default for -key; the web UI reads only this variable
// (no key in the browser).
const EnvISBNAPIKey = "ISBN_API_KEY"

// These environment variables MAY override values from .isbn_config.yml when
// set. CLI flags override both file and environment. Names use a FANTASTIC_SPOON_
// prefix except legacy ISBNDB_BOOKS_URL, which is accepted as an alias for the books URL.
//
// Normative terms follow RFC 2119 (https://www.rfc-editor.org/rfc/rfc2119).
const (
	EnvInputFile       = "FANTASTIC_SPOON_INPUT"
	EnvOutputFile      = "FANTASTIC_SPOON_OUTPUT"
	EnvBatchSize       = "FANTASTIC_SPOON_BATCH"
	EnvBooksURL        = "FANTASTIC_SPOON_API_URL"
	EnvRateEvery       = "FANTASTIC_SPOON_RATE_EVERY"
	EnvWeb             = "FANTASTIC_SPOON_WEB"
	EnvListen          = "FANTASTIC_SPOON_LISTEN"
	EnvReadTimeout     = "FANTASTIC_SPOON_READ_TIMEOUT"
	EnvWriteTimeout    = "FANTASTIC_SPOON_WRITE_TIMEOUT"
	EnvIdleTimeout     = "FANTASTIC_SPOON_IDLE_TIMEOUT"
	EnvCollectionFile  = "FANTASTIC_SPOON_COLLECTION"
	EnvStatusLogFile   = "FANTASTIC_SPOON_STATUS_LOG"
	EnvBookURLTemplate = "FANTASTIC_SPOON_BOOK_URL_TEMPLATE"
)
