package providersignal

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"
)

// QuotaEventUnreadable is the generation of a quota signal that exists but
// cannot be read: its content, hence its event, is unknown.
const QuotaEventUnreadable = "unreadable"

// quotaGenerationBytes is the prefix of the content digest kept as a generation.
const quotaGenerationBytes = 8

// QuotaEvent identifies one quota exhaustion event of a provider: one write of
// its signal file. Every raise rewrites the file with a fresh detection time,
// so a re-raise announcing the same reset is a new generation, while reading
// or expiring the signal leaves it unchanged. Detection is recorded to the
// second: raises in the same second with the same message and reset write
// identical bytes and share a generation.
type QuotaEvent struct {
	Provider string
	// Generation is a digest of the signal file's bytes, or
	// QuotaEventUnreadable.
	Generation string
	// Detected is when the exhaustion was detected (the file's mtime when the
	// record names none); zero when the file cannot be read.
	Detected time.Time
	// Message is the provider's quota message, when recorded.
	Message string
}

// QuotaSignalEvent reports the quota event behind provider's signal file. ok is
// false when no signal file exists. It reads only: whether the event still
// blocks starts is QuotaBlockedUntil's answer.
func QuotaSignalEvent(projectRoot, provider string) (QuotaEvent, bool) {
	provider = canonicalQuotaProvider(provider)
	path := QuotaSignalPath(projectRoot, provider)
	info, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		return QuotaEvent{}, false
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return QuotaEvent{}, false
	}
	if statErr != nil || err != nil {
		return QuotaEvent{Provider: provider, Generation: QuotaEventUnreadable}, true
	}
	digest := sha256.Sum256(data)
	fields := parseRecord(string(data))
	detected, parseErr := time.Parse(time.RFC3339, fields[recordKeyDetected])
	if parseErr != nil {
		detected = info.ModTime().UTC().Truncate(time.Second)
	}
	return QuotaEvent{
		Provider:   provider,
		Generation: hex.EncodeToString(digest[:quotaGenerationBytes]),
		Detected:   detected.UTC(),
		Message:    fields[recordKeyMessage],
	}, true
}
