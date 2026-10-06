package models

import "time"

// TerminalArchiveRef identifies one durable immutable logical task record.
// Physical task rows keep identity/status/created plus this reference; all
// ordinary database readers restore the complete record (ADR-0183).
type TerminalArchiveRef struct {
	SHA256     string    `yaml:"sha256" json:"sha256"`
	ArchivedAt time.Time `yaml:"archived_at" json:"archived_at"`
}
