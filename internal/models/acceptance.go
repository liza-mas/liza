package models

import (
	"time"

	"github.com/liza-mas/liza/internal/referencecontract"
)

// AcceptanceSource records the immutable, independently reviewed allocation
// adopted by a task. It survives retries so strict admission cannot downgrade.
type AcceptanceSource struct {
	Ref                string `yaml:"ref" json:"ref"`
	Commit             string `yaml:"commit" json:"commit"`
	Blob               string `yaml:"blob" json:"blob"`
	ParentTask         string `yaml:"parent_task" json:"parent_task"`
	ParentReviewCommit string `yaml:"parent_review_commit" json:"parent_review_commit"`
}

// AcceptanceCommandResult records one trusted execution. Command and Output
// are masked presentation; CommandSHA256 binds the exact original command.
type AcceptanceCommandResult struct {
	Command       string    `yaml:"command" json:"command"`
	CommandSHA256 string    `yaml:"command_sha256" json:"command_sha256"`
	ExitCode      int       `yaml:"exit_code" json:"exit_code"`
	StartedAt     time.Time `yaml:"started_at" json:"started_at"`
	FinishedAt    time.Time `yaml:"finished_at" json:"finished_at"`
	Output        string    `yaml:"output" json:"output"`
}

// AcceptanceReceipt binds validated mappings and successful executions to one
// immutable review commit. Admission stores it atomically with review state.
type AcceptanceReceipt struct {
	Version      int                                   `yaml:"version" json:"version"`
	ReviewCommit string                                `yaml:"review_commit" json:"review_commit"`
	Source       AcceptanceSource                      `yaml:"source" json:"source"`
	ManifestPath string                                `yaml:"manifest_path" json:"manifest_path"`
	ManifestBlob string                                `yaml:"manifest_blob" json:"manifest_blob"`
	Mappings     []referencecontract.AcceptanceMapping `yaml:"mappings" json:"mappings"`
	Commands     []AcceptanceCommandResult             `yaml:"commands" json:"commands"`
}
