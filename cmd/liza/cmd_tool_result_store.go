package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

const (
	toolResultDefaultThresholdBytes = 32768
	toolResultDefaultDigestBytes    = 4096
	toolResultMinBudgetBytes        = 1024
	// The OpenCode boundary has the same documented 64 KiB safety ceiling.
	toolResultMaxThresholdBytes = 65536
)

// toolResultStoreResolver returns the per-invocation store factory shared by
// every tool-result subcommand. The group's persistent flags fill root and
// config; an environment budget applies only to a flag the caller left unset.
func toolResultStoreResolver(root *string, config *toolresult.Config) func(*cobra.Command) (*toolresult.Store, error) {
	return func(cmd *cobra.Command) (*toolresult.Store, error) {
		resolved := *config
		if err := applyToolResultBudgetEnv(cmd, &resolved); err != nil {
			return nil, err
		}
		if resolved.ThresholdBytes < toolResultMinBudgetBytes || resolved.ThresholdBytes > toolResultMaxThresholdBytes || resolved.DigestBytes < toolResultMinBudgetBytes {
			return nil, fmt.Errorf("budget settings require %d <= digest-bytes <= threshold-bytes <= %d", toolResultMinBudgetBytes, toolResultMaxThresholdBytes)
		}
		dir, err := toolResultArtifactDir(cmd, *root)
		if err != nil {
			return nil, err
		}
		return toolresult.New(dir, resolved, nil)
	}
}

func applyToolResultBudgetEnv(cmd *cobra.Command, resolved *toolresult.Config) error {
	for _, option := range []struct {
		flag, suffix string
		value        *int
	}{
		{"threshold-bytes", "TOOL_RESULT_THRESHOLD_BYTES", &resolved.ThresholdBytes},
		{"digest-bytes", "TOOL_RESULT_DIGEST_BYTES", &resolved.DigestBytes},
	} {
		if cmd.Flags().Changed(option.flag) {
			continue
		}
		if raw := brand.LookupEnv(os.Getenv, option.suffix).Value; raw != "" {
			number, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("invalid %s budget setting", option.flag)
			}
			*option.value = number
		}
	}
	return nil
}

func toolResultArtifactDir(cmd *cobra.Command, root string) (string, error) {
	if root != "" {
		return root, nil
	}
	project, _ := cmd.Flags().GetString("project-root")
	if project == "" {
		var err error
		project, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(project, paths.ProjectDirName(), "tool-results"), nil
}
