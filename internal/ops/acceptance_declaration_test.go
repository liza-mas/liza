package ops

import (
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// A bounded reader's own refusal is a pure function of the committed bytes and
// classifies as content; any other reader error is a failed Git read, whose
// zero class a supervisor never escalates.
func TestReadRefusalClassSeparatesContentFromFailedReads(t *testing.T) {
	t.Parallel()
	if got := readRefusalClass(&acceptanceArtifactRefusal{reason: "too large"}); got != AcceptanceFaultContent {
		t.Errorf("reader refusal class = %q, want %q", got, AcceptanceFaultContent)
	}
	if got := readRefusalClass(errors.New("git failed")); got != "" {
		t.Errorf("failed read class = %q, want the zero class", got)
	}
}

func TestAcceptanceSourceRefusalNamesTaskFieldAndClass(t *testing.T) {
	t.Parallel()
	err := acceptanceSourceRefusal(&models.Task{ID: "task-9"}, AcceptanceFaultAllocation, "why")
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) {
		t.Fatalf("not an acceptance error: %v", err)
	}
	if refusal.TaskID != "task-9" || refusal.Field != "acceptance.source" || refusal.Reason != "why" || refusal.Class != AcceptanceFaultAllocation || refusal.Claim != nil {
		t.Errorf("refusal = %+v", refusal)
	}
}

// Named values keep the numbers and spellings the code used inline.
func TestAcceptanceNamedValuesKeepTheirValues(t *testing.T) {
	t.Parallel()
	ints := map[string][2]int64{
		"default acceptance timeout seconds": {defaultAcceptanceTimeoutSeconds, 600},
		"command wait delay":                 {int64(acceptanceCommandWaitDelay), int64(5 * time.Second)},
		"unknown exit code":                  {acceptanceExitUnknown, -1},
		"receipt version":                    {acceptanceReceiptVersion, 1},
		"diagnostic schema version":          {acceptanceDiagnosticSchemaVersion, 1},
		"creation commit length":             {acceptanceCreationCommitLength, 12},
		"ownership poll interval":            {int64(defaultAcceptanceOwnershipPollInterval), int64(15 * time.Second)},
		"ownership poll variable":            {int64(acceptanceOwnershipPollInterval), int64(15 * time.Second)},
		"output limit":                       {acceptanceOutputLimit, 1 << 20},
		"failure excerpt limit":              {acceptanceFailureExcerptLimit, 8 << 10},
		"archive default tasks":              {int64(DefaultArchiveLimit.MaxTasks), 8},
		"archive default bytes":              {int64(DefaultArchiveLimit.MaxBytes), 4 << 20},
	}
	for name, v := range ints {
		if v[0] != v[1] {
			t.Errorf("%s = %d, want %d", name, v[0], v[1])
		}
	}
	strs := map[string][2]string{
		"masked value":              {acceptanceMaskedValue, "***"},
		"secret entry name":         {acceptanceSecretEntryName, "SECRET_ACCEPTANCE"},
		"content reason class":      {acceptanceReasonContentInvalid, "acceptance_evidence_invalid"},
		"allocation reason class":   {acceptanceReasonAllocationRepeated, "acceptance_claim_refused_repeatedly"},
		"claim refusal key":         {claimRefusalExtraKey, "claim_refusal"},
		"claim refusal value":       {claimRefusalAcceptanceValue, "acceptance_evidence"},
		"fault class key":           {faultClassExtraKey, "fault_class"},
		"integration commit key":    {integrationCommitExtraKey, "integration_commit"},
		"remap replaced key":        {remapReplacedExtraKey, "replaced"},
		"remap integration key":     {remapIntegrationExtraKey, "integration"},
		"remap history entries key": {remapHistoryEntriesExtraKey, "history_entries"},
		"regular file mode":         {gitModeRegularFile, "100644"},
		"executable file mode":      {gitModeExecutableFile, "100755"},
	}
	for name, v := range strs {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", name, v[0], v[1])
		}
	}
	if acceptanceBatchTimeout(0) != 600*time.Second || acceptanceBatchTimeout(7) != 7*time.Second {
		t.Errorf("batch timeout: zero grants %s, 7 grants %s", acceptanceBatchTimeout(0), acceptanceBatchTimeout(7))
	}
}
