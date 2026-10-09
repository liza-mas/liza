package ops

import (
	"crypto/sha256"
	"fmt"
	"log"
	"path/filepath"
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

// Agent-pool sizing keys settable after init. They are operator-only: an
// agent must not resize its own pool.
const (
	MaxInstancesConfigKey    = "config.max_instances"
	DoerMaxWaitConfigKey     = "config.doer_max_wait"
	ReviewerMaxWaitConfigKey = "config.reviewer_max_wait"
)

// MinPoolMaxWaitSeconds is the smallest accepted doer/reviewer idle wait.
const MinPoolMaxWaitSeconds = 300

// PoolConfigKeys lists the integer agent-pool keys in display order.
var PoolConfigKeys = []string{MaxInstancesConfigKey, DoerMaxWaitConfigKey, ReviewerMaxWaitConfigKey}

// SetPoolConfigInput describes an operator write of one agent-pool key.
type SetPoolConfigInput struct {
	Key     string
	Value   int
	Replace bool
	Reason  string
}

func poolConfigField(config *models.Config, key string) (*int, int, bool) {
	switch key {
	case MaxInstancesConfigKey:
		return &config.MaxInstances, 1, true
	case DoerMaxWaitConfigKey:
		return &config.DoerMaxWait, MinPoolMaxWaitSeconds, true
	case ReviewerMaxWaitConfigKey:
		return &config.ReviewerMaxWait, MinPoolMaxWaitSeconds, true
	}
	return nil, 0, false
}

// SetPoolConfig stores one agent-pool key under the same rule as other
// runtime configuration: compare and write in one transaction, and replacing
// a different set value requires --replace with --reason. Zero means unset.
// Running supervisors keep the wait they read at start.
func SetPoolConfig(projectRoot string, input SetPoolConfigInput) (*ConfigSetResult, error) {
	return setIntegerConfig(projectRoot, input, poolConfigField)
}

// SetAwaitPollInterval stores the shared await interval on the operator path.
// Existing awaits keep their entry value; the next invocation reads the change.
func SetAwaitPollInterval(projectRoot string, value int, replace bool, reason string) (*ConfigSetResult, error) {
	if value < 1 {
		return nil, &PreconditionError{Reason: fmt.Sprintf("%s must be at least 1", AwaitPollIntervalConfigKey)}
	}
	if _, err := awaitPollInterval(value); err != nil {
		return nil, err
	}
	return setIntegerConfig(projectRoot, SetPoolConfigInput{
		Key: AwaitPollIntervalConfigKey, Value: value, Replace: replace, Reason: reason,
	}, func(config *models.Config, key string) (*int, int, bool) {
		return &config.AwaitPollInterval, 1, key == AwaitPollIntervalConfigKey
	})
}

// setIntegerConfig shares compare/write/audit semantics without widening the
// supported keys of the existing pool configuration API.
func setIntegerConfig(projectRoot string, input SetPoolConfigInput, fieldFor func(*models.Config, string) (*int, int, bool)) (*ConfigSetResult, error) {
	var probe models.Config
	if _, minimum, ok := fieldFor(&probe, input.Key); !ok {
		return nil, &PreconditionError{Reason: fmt.Sprintf("unsupported agent-pool config key %q", input.Key)}
	} else if input.Value < minimum {
		return nil, &PreconditionError{Reason: fmt.Sprintf("%s must be at least %d", input.Key, minimum)}
	}
	if input.Replace && strings.TrimSpace(input.Reason) == "" {
		return nil, &PreconditionError{Reason: "--replace requires a non-empty --reason"}
	}

	bb := db.For(paths.New(projectRoot).StatePath())
	result := &ConfigSetResult{Key: input.Key}
	previous := 0
	err := bb.Modify(func(state *models.State) error {
		field, _, _ := fieldFor(&state.Config, input.Key)
		previous = *field
		switch {
		case previous == input.Value:
			result.Outcome = "unchanged"
		case previous != 0 && !input.Replace:
			result.Outcome = "conflict"
			return &PreconditionError{
				Reason:  fmt.Sprintf("%s is already set to %d; use --replace with --reason to replace it", input.Key, previous),
				Details: map[string]any{"key": input.Key, "conflict": "existing_value"},
			}
		default:
			*field = input.Value
			result.Outcome = "set"
			if previous != 0 {
				result.Outcome = "replaced"
			}
		}
		return nil
	})
	if err != nil && result.Outcome != "conflict" {
		result.Outcome = "failed"
	}
	mask := secretmask.New()
	log.Printf("config_set key=%s outcome=%s actor=%q project=%q previous=%d value=%d reason=%q",
		input.Key, result.Outcome, "operator", mask.MaskText(projectRoot), previous, input.Value, boundPostWorktreeCmd(mask.MaskText(input.Reason)))
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", input.Key, err)
	}
	return result, nil
}

// RuntimeInputRegistryConfigKey names the repository-relative recipe registry
// runtime-input declarations resolve against (ADR-0169). Operator-only: the
// registry decides which recipes a plan may name.
const RuntimeInputRegistryConfigKey = "config.runtime_input_registry"

// SetRuntimeInputRegistryInput describes an operator write of the registry path.
type SetRuntimeInputRegistryInput struct {
	Path    string
	Replace bool
	Reason  string
}

// SetRuntimeInputRegistry stores the registry path under the shared config
// rule: compare and write in one transaction; replacing a different value
// requires --replace with --reason. The file itself is read at the
// integration commit when a declaration is admitted, not here.
func SetRuntimeInputRegistry(projectRoot string, input SetRuntimeInputRegistryInput) (*ConfigSetResult, error) {
	if err := validateRuntimeInputRegistryPath(input.Path); err != nil {
		return nil, err
	}
	if input.Replace && strings.TrimSpace(input.Reason) == "" {
		return nil, &PreconditionError{Reason: "--replace requires a non-empty --reason"}
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	result := &ConfigSetResult{Key: RuntimeInputRegistryConfigKey}
	previous := ""
	err := bb.Modify(func(state *models.State) error {
		previous = state.Config.RuntimeInputRegistry
		switch {
		case previous == input.Path:
			result.Outcome = "unchanged"
		case previous != "" && !input.Replace:
			result.Outcome = "conflict"
			return &PreconditionError{
				Reason:  RuntimeInputRegistryConfigKey + " is already set to a different path; use --replace with --reason to replace it",
				Details: map[string]any{"key": RuntimeInputRegistryConfigKey, "conflict": "existing_value"},
			}
		default:
			state.Config.RuntimeInputRegistry = input.Path
			result.Outcome = "set"
			if previous != "" {
				result.Outcome = "replaced"
			}
		}
		return nil
	})
	if err != nil && result.Outcome != "conflict" {
		result.Outcome = "failed"
	}
	mask := secretmask.New()
	log.Printf("config_set key=%s outcome=%s actor=%q project=%q previous=%q value=%q reason=%q",
		RuntimeInputRegistryConfigKey, result.Outcome, "operator", mask.MaskText(projectRoot), previous, input.Path, boundPostWorktreeCmd(mask.MaskText(input.Reason)))
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", RuntimeInputRegistryConfigKey, err)
	}
	return result, nil
}

// validateRuntimeInputRegistryPath accepts one clean repository-relative
// file path, the shape Git reads at a commit.
func validateRuntimeInputRegistryPath(value string) error {
	if value == "" || !filepath.IsLocal(value) || filepath.ToSlash(filepath.Clean(value)) != value ||
		strings.ContainsAny(value, "\\\r\n\x00") || !utf8.ValidString(value) {
		return &PreconditionError{Reason: RuntimeInputRegistryConfigKey + " must be one clean repository-relative file path"}
	}
	return nil
}
