package models

// AdmissionTimeoutRetry is the operator's standing policy for a submit whose
// canonical batch runs out of its own budget: re-run the whole batch once,
// in the same submit, at BudgetFactor times the batch budget, never above
// MaxTimeoutSeconds (0 means the engine cap). Ruling names the human decision
// that authorizes it (ADR-0216). Absent means no retry.
type AdmissionTimeoutRetry struct {
	BudgetFactor      int    `yaml:"budget_factor" json:"budget_factor"`
	MaxTimeoutSeconds int    `yaml:"max_timeout_seconds,omitempty" json:"max_timeout_seconds,omitempty"`
	Ruling            string `yaml:"ruling" json:"ruling"`
}

// Bounds that can make a retry budget smaller than the configured one.
const (
	AdmissionRetryClippedByMax      = "max_timeout_seconds"
	AdmissionRetryClippedByCap      = "engine cap"
	AdmissionRetryClippedByEnvelope = "submit envelope"
)

// AcceptanceTimeoutRetry records, on the receipt it produced, that admission
// re-ran the whole batch after the first run hit its budget (ADR-0216).
// ConfiguredTimeoutSeconds is factor times TimeoutSeconds before clipping;
// ClippedBy names the bound that lowered it, empty when none did.
type AcceptanceTimeoutRetry struct {
	TimedOutIndex            int    `yaml:"timed_out_index" json:"timed_out_index"`
	TimeoutSeconds           int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	RetryTimeoutSeconds      int    `yaml:"retry_timeout_seconds" json:"retry_timeout_seconds"`
	ConfiguredTimeoutSeconds int    `yaml:"configured_timeout_seconds" json:"configured_timeout_seconds"`
	ClippedBy                string `yaml:"clipped_by,omitempty" json:"clipped_by,omitempty"`
	Ruling                   string `yaml:"ruling" json:"ruling"`
}
