package paths

import (
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestLifecycleMetricsDirUsesBrand(t *testing.T) {
	previous := brand.ProjectDirName
	brand.ProjectDirName = ".counter-brand"
	t.Cleanup(func() { brand.ProjectDirName = previous })
	root := t.TempDir()
	if got, want := New(root).LifecycleMetricsDir(), filepath.Join(root, ".counter-brand", "lifecycle-metrics"); got != want {
		t.Fatalf("metrics directory=%q, want %q", got, want)
	}
}
