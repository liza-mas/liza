package semble

import (
	"strings"
	"time"
)

// PromptMetadataOptions configures prompt-safe Semble metadata construction.
type PromptMetadataOptions struct {
	Kind                 TargetKind
	TargetRoot           string
	ExpectedWorktreeRoot string
	LookPath             ExecutableLookup
	Runner               CommandRunner
	Timeout              time.Duration
	Fixture              FixtureIdentity
}

// PromptMetadata is safe for prompt rendering; it intentionally excludes
// diagnostics, command output, executable paths, and cache paths.
type PromptMetadata struct {
	TargetRoot      string
	ShellTargetRoot string
}

// BuildPromptMetadata returns prompt-safe Semble context only when activation,
// target safety, model readiness, and actual repository readiness all pass.
func BuildPromptMetadata(opts PromptMetadataOptions) (PromptMetadata, bool) {
	if !RuntimeEnabled() {
		return PromptMetadata{}, false
	}
	safety := ValidateTargetSafety(TargetSafetyOptions{
		Kind:                 opts.Kind,
		TargetRoot:           opts.TargetRoot,
		ExpectedWorktreeRoot: opts.ExpectedWorktreeRoot,
	})
	if !safety.Safe {
		return PromptMetadata{}, false
	}
	readiness := CheckOfflineReadiness(ValidationOptions{
		TargetRoot: safety.TargetRoot,
		LookPath:   opts.LookPath,
		Runner:     opts.Runner,
		Timeout:    opts.Timeout,
		Fixture:    opts.Fixture,
	})
	if !readiness.Ready {
		return PromptMetadata{}, false
	}
	corpus := CheckRepositoryReadiness(ValidationOptions{
		TargetRoot: safety.TargetRoot,
		LookPath:   opts.LookPath,
		Runner:     opts.Runner,
		Timeout:    opts.Timeout,
	})
	if !corpus.Ready {
		return PromptMetadata{}, false
	}

	shellTargetRoot := shellQuote(safety.TargetRoot)
	return PromptMetadata{
		TargetRoot:      safety.TargetRoot,
		ShellTargetRoot: shellTargetRoot,
	}, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
