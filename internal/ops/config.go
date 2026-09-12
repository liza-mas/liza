package ops

import (
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/projectdetect"
	"github.com/liza-mas/liza/internal/secretmask"
)

// PostWorktreeConfigKey is shared by the config CLI and general state queries.
const PostWorktreeConfigKey = "config.post_worktree_cmd"

// SetPostWorktreeCmdInput describes an explicit configuration write. A nil
// Authority selects the operator path; identified agents are generation-fenced.
type SetPostWorktreeCmdInput struct {
	Command   string
	Replace   bool
	Reason    string
	Authority *models.AgentAuthority
}

// ConfigSetResult reports the committed outcome without echoing shell text.
type ConfigSetResult struct {
	Key     string `json:"key"`
	Outcome string `json:"outcome"`
}

// SetPostWorktreeCmd stores a command without executing it. Comparison and
// mutation share one blackboard transaction, including the authority check.
// Audit records go only to the process log, not the blackboard or activity log.
func SetPostWorktreeCmd(projectRoot string, input SetPostWorktreeCmdInput) (*ConfigSetResult, error) {
	if input.Command == "" || strings.TrimSpace(input.Command) != input.Command ||
		strings.ContainsAny(input.Command, "\r\n\x00") || !utf8.ValidString(input.Command) {
		return nil, &PreconditionError{Reason: "command must be non-empty, single-line UTF-8 without NUL or surrounding whitespace"}
	}
	if input.Replace && strings.TrimSpace(input.Reason) == "" {
		return nil, &PreconditionError{Reason: "--replace requires a non-empty --reason"}
	}

	// This is observational evidence, not an activation decision. A mismatch
	// includes conventional layouts the current detector does not yet support.
	detected := projectdetect.DetectPostWorktreeCmd(projectRoot)
	detection := "different"
	if detected == "" {
		detection = "none"
	} else if detected == input.Command {
		detection = "match"
	}

	bb := db.For(paths.New(projectRoot).StatePath())
	result := &ConfigSetResult{Key: PostWorktreeConfigKey}
	err := lifecycleMutation(bb, input.Authority)(func(state *models.State) error {
		current := state.Config.PostWorktreeCmd
		switch {
		case current != nil && *current == input.Command:
			result.Outcome = "unchanged"
		case current != nil && !input.Replace:
			result.Outcome = "conflict"
			return &PreconditionError{
				Reason:  PostWorktreeConfigKey + " is already set to a different command; use --replace with --reason to replace it",
				Details: map[string]any{"key": PostWorktreeConfigKey, "conflict": "existing_value"},
			}
		default:
			command := input.Command
			state.Config.PostWorktreeCmd = &command
			result.Outcome = "set"
			if current != nil {
				result.Outcome = "replaced"
			}
		}
		return nil
	})
	if err != nil && result.Outcome != "conflict" {
		result.Outcome = "failed"
	}
	actor := "operator"
	if input.Authority != nil {
		actor = input.Authority.ID
	}
	mask := secretmask.New()
	log.Printf("config_set key=%s outcome=%s actor=%q project=%q detector=%s command_sha256=%x command=%q reason=%q",
		PostWorktreeConfigKey, result.Outcome, mask.MaskText(actor), mask.MaskText(projectRoot), detection,
		sha256.Sum256([]byte(input.Command)), boundPostWorktreeCmd(mask.MaskText(input.Command)), boundPostWorktreeCmd(mask.MaskText(input.Reason)))
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", PostWorktreeConfigKey, err)
	}
	return result, nil
}
