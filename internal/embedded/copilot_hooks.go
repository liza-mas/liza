package embedded

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/brand"
)

var copilotHookContents = map[string][]byte{
	"enforce-init.sh":        enforceInitHookContent,
	"session-context.sh":     sessionContextHookContent,
	"git-guard.sh":           gitGuardHookContent,
	"rtk-guard.sh":           rtkGuardHookContent,
	"worktree-path-guard.sh": worktreePathGuardHookContent,
}

// CopilotHooksFileName returns the repo hook config filename Liza manages
// under .github/hooks/ for the current brand.
func CopilotHooksFileName() string {
	return brand.BinaryName + ".json"
}

// WriteCopilotHooks writes embedded hook scripts to .copilot/hooks/ in the
// project root. Always overwrites — hooks are Liza infrastructure, not
// user-customizable.
func WriteCopilotHooks(projectRoot string) error {
	hooksDir := filepath.Join(projectRoot, ".copilot", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .copilot/hooks directory: %w", err)
	}

	for name, content := range copilotHookContents {
		hookPath := filepath.Join(hooksDir, name)
		if err := os.WriteFile(hookPath, renderEmbeddedAsset(content), 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}

// WriteCopilotProjectHooks writes the repo-local Copilot hook configuration to
// .github/hooks/<binary>.json and deploys the shared hook scripts to
// .copilot/hooks/. The config file is Liza-owned — it lives in a dedicated
// filename so it never collides with user hook files.
func WriteCopilotProjectHooks(projectRoot string, reader *bufio.Reader) error {
	return WriteCopilotProjectHooksWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteCopilotProjectHooksWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	if err := WriteCopilotHooks(projectRoot); err != nil {
		return err
	}
	hooksDir := filepath.Join(projectRoot, ".github", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .github/hooks directory: %w", err)
	}
	configPath := filepath.Join(hooksDir, CopilotHooksFileName())
	rendered := renderEmbeddedAsset(copilotHooksContent)
	if existing, err := os.ReadFile(configPath); err == nil && !bytes.Equal(existing, rendered) {
		ok, err := confirmMergeWithOptions(fmt.Sprintf("%s exists and differs from the managed %s Copilot hooks. Overwrite? (y/n): ", configPath, brand.NameTitle), reader, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}
	if err := os.WriteFile(configPath, rendered, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	return nil
}
