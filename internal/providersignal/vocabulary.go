package providersignal

import (
	"os"

	"github.com/liza-mas/liza/internal/alerts"
)

// Signal record vocabulary. A signal file is "key: value" lines; the writers
// and readers below share these spellings with the files already on disk.
const (
	recordSeparator     = ": "
	recordKeyProvider   = "provider"
	recordKeyDetected   = "detected"
	recordKeyMessage    = "message"
	recordKeyResetsAt   = "resets_at"
	recordKeyExpires    = "expires"
	resetsAtUnannounced = "unknown"
)

// signalFileMode is the permission of signal files and the alerts log.
const signalFileMode os.FileMode = 0o644

// alertLevel is the level of every alert this package raises.
const alertLevel = string(alerts.AlertLevelCritical)

// Alert categories this package writes, spelled as they appear in alerts.log.
const (
	alertCategoryQuotaExhausted          = "PROVIDER QUOTA EXHAUSTED"
	alertCategoryQuotaSpawnBlocked       = "PROVIDER QUOTA SPAWN BLOCKED"
	alertCategoryUnavailable             = "PROVIDER UNAVAILABLE"
	alertCategoryUnavailableSpawnBlocked = "PROVIDER UNAVAILABLE SPAWN BLOCKED"
)

// Provider stream-event vocabulary the detectors read. These are the providers'
// own spellings, not ours.
const (
	eventTypeError          = "error"
	eventTypeTurnFailed     = "turn.failed"
	eventTypeResult         = "result"
	eventTypeRateLimit      = "rate_limit_event"
	rateLimitStatusRejected = "rejected"
	// rateLimitedAPIStatus is the API status of a throttled request.
	rateLimitedAPIStatus = 429
)

// Leading characters that mark a provider output line as a JSON object or array
// event rather than plain text.
const (
	jsonObjectLinePrefix = "{"
	jsonArrayLinePrefix  = "["
)

// recordLine is one "key: value" line of a signal record.
func recordLine(key, value string) string {
	return key + recordSeparator + value + "\n"
}
