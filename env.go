package main

// EnvISBNAPKey is the environment variable name for the books API credential.
// The CLI reads it as the default for -key; the web UI reads only this variable
// (no key in the browser).
const EnvISBNAPKey = "ISBN_AP_KEY"

// Optional environment variables override values from .isbn_config.yml (when
// present). CLI flags override both. Names use a FANTASTIC_SPOON_ prefix except
// legacy ISBNDB_BOOKS_URL, which is accepted as an alias for the books URL.
const (
	EnvInputFile       = "FANTASTIC_SPOON_INPUT"
	EnvOutputFile      = "FANTASTIC_SPOON_OUTPUT"
	EnvBatchSize       = "FANTASTIC_SPOON_BATCH"
	EnvBooksURL        = "FANTASTIC_SPOON_API_URL"
	EnvRateEvery       = "FANTASTIC_SPOON_RATE_EVERY"
	EnvWeb             = "FANTASTIC_SPOON_WEB"
	EnvListen          = "FANTASTIC_SPOON_LISTEN"
	EnvCollectionFile  = "FANTASTIC_SPOON_COLLECTION"
	EnvStatusLogFile   = "FANTASTIC_SPOON_STATUS_LOG"
	EnvBookURLTemplate = "FANTASTIC_SPOON_BOOK_URL_TEMPLATE"
)
