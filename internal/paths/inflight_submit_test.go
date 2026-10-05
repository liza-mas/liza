package paths

import (
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestInflightSubmitDirUsesBrand(t *testing.T) {
	previous := brand.ProjectDirName
	brand.ProjectDirName = ".counter-brand"
	t.Cleanup(func() { brand.ProjectDirName = previous })
	root := t.TempDir()
	if got, want := New(root).InflightSubmitDir(), filepath.Join(root, ".counter-brand", "inflight-submit"); got != want {
		t.Fatalf("in-flight submit directory=%q, want %q", got, want)
	}
}
