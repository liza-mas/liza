package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// integrationTestWaitDelay bounds how long cmd.Wait lingers once the integration
// test is killed, when child processes still hold its pipes open.
const integrationTestWaitDelay = 5 * time.Second

// integrationScriptRun is what running the project's integration test script
// observed. output is set for a pass as well as a failure.
type integrationScriptRun struct {
	ran      bool
	notFound bool
	failed   bool
	output   string
}

// validateArtifacts checks, after the ref advanced, that every protected
// artifact ref still resolves. A broken ref rewinds the invocation's own merge
// and fails the task.
func (m *mergeInvocation) validateArtifacts() error {
	if artifactGuardPostUpdateTestHook != nil {
		if err := artifactGuardPostUpdateTestHook(); err != nil {
			return fmt.Errorf("artifact guard post-update test hook failed: %w", err)
		}
	}

	currentState, err := m.bb.Read()
	if err != nil {
		return fmt.Errorf("failed to read state for post-merge artifact validation: %w", err)
	}
	validationErr := statevalidate.ValidateMergeArtifactRefs(currentState, m.projectRoot, m.taskID)
	if validationErr == nil {
		return nil
	}
	rewound, rollbackErr := m.rollbackOwnMerge()
	detail := validationErr.Error()
	if retained := integrationRetentionDetail(rewound, rollbackErr, m.mergeCommit); retained != "" {
		detail = detail + "; " + retained
	}
	diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonStateInvalid, detail, m.mergeCommit, "", rollbackErr)
	if updateErr := m.recordFailure(IntegrationReasonStateInvalid, m.mergeCommit, diagnostic, m.request); updateErr != nil {
		return updateErr
	}
	return &IntegrationFailedError{
		Reason:        IntegrationReasonStateInvalid,
		RollbackError: rollbackErr,
	}
}

// rollbackOwnMerge rewinds the integration ref only if it still points to this
// invocation's merge commit. If someone else merged on top, rewinding would drop
// their work.
func (m *mergeInvocation) rollbackOwnMerge() (bool, error) {
	return rollbackMergedCommitAndPersist(m.bb, m.projectRoot, m.git, m.integrationRef, m.rollbackBaseline, m.mergeCommit, m.restoreRef, m.taskID, m.authority)
}

// runIntegrationTests runs the project's integration test script, when it has
// one, against the merged ref. A failing script rewinds the invocation's own
// merge and fails the task with the script's output.
func (m *mergeInvocation) runIntegrationTests() error {
	m.tests = runIntegrationScript(m.projectRoot, m.taskID)
	if !m.tests.failed {
		return nil
	}
	rewound, rollbackErr := m.rollbackOwnMerge()
	diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonTestsFailed,
		integrationRetentionDetail(rewound, rollbackErr, m.mergeCommit), m.mergeCommit, m.tests.output, rollbackErr)
	if updateErr := m.recordFailure(IntegrationReasonTestsFailed, m.mergeCommit, diagnostic, m.request); updateErr != nil {
		return updateErr
	}
	return &IntegrationFailedError{
		Reason:        IntegrationReasonTestsFailed,
		TestOutput:    m.tests.output,
		RollbackError: rollbackErr,
	}
}

// runIntegrationScript executes integrationTestCommand from the project root. A
// missing or unstattable script is logged and the merge proceeds without tests.
func runIntegrationScript(projectRoot, taskID string) integrationScriptRun {
	script := filepath.Join(projectRoot, integrationTestCommand)
	_, statErr := statIntegrationScript(script)
	if errors.Is(statErr, os.ErrNotExist) {
		// Integration test script not found — log warning for audit trail.
		log.Printf("wt-merge %s: WARNING — integration test script not found at %s, proceeding without tests", taskID, script)
		return integrationScriptRun{notFound: true}
	}
	if statErr != nil {
		// Distinguish actual stat failures from true missing-script cases.
		log.Printf("wt-merge %s: WARNING — unable to stat integration test script at %s: %v; proceeding without tests", taskID, script, statErr)
		return integrationScriptRun{}
	}

	var combinedOutput bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), DefaultIntegrationTestTimeout)
	defer cancel()
	// Hand the script to a shell rather than exec it directly: it is a POSIX
	// script, and Windows cannot fork/exec one — the run failed there with no
	// output at all, so a project's integration tests never ran. The command
	// stays repo-relative and the shell resolves it against cmd.Dir, which
	// keeps a native path out of a shell string.
	cmd, runErr := shellCommandContext(ctx, integrationTestCommand, projectRoot)
	if runErr == nil {
		cmd.Stdout = &combinedOutput
		cmd.Stderr = &combinedOutput
		// Kill the entire process tree on timeout on both Unix and Windows.
		// WaitDelay ensures cmd.Wait returns even if child processes hold
		// pipes open after kill.
		configProcessGroupKill(cmd)
		cmd.WaitDelay = integrationTestWaitDelay
		runErr = cmd.Run()
	} else {
		combinedOutput.WriteString(runErr.Error())
	}

	run := integrationScriptRun{ran: true, failed: runErr != nil, output: combinedOutput.String()}
	if run.failed && ctx.Err() == context.DeadlineExceeded {
		run.output += fmt.Sprintf("\n[%s] integration test killed after %s timeout", brand.BinaryName, DefaultIntegrationTestTimeout)
	}
	return run
}
