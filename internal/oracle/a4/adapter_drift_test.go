package a4

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/ops"
)

func init() {
	helperOps["obligation-drift"] = func(root string) (string, error) {
		head, err := exec.Command("git", "-C", root, "rev-parse", "integration").Output()
		if err != nil {
			return "", fmt.Errorf("resolve integration: %w", err)
		}
		recorded, err := ops.RecordObligationContentDrift(
			db.New(filepath.Join(root, ".liza", "state.yaml")), root, strings.TrimSpace(string(head)), "coder-1")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("recorded: %v", recorded), nil
	}
}
