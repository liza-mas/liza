package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func autoResumeLogicalState(t *testing.T, state *models.State) map[string]any {
	t.Helper()
	data, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	// The publication counter is native bookkeeping, not execution state.
	delete(result, "mutation_sequence")
	delete(result["config"].(map[string]any), "auto_resume")
	return result
}

func TestSetAutoResumeWithResultPreservesExecution(t *testing.T) {
	for _, mode := range []models.SystemMode{models.SystemModeRunning, models.SystemModePaused, models.SystemModeStopped} {
		for _, sprint := range []models.SprintStatus{models.SprintStatusInProgress, models.SprintStatusCheckpoint, models.SprintStatusCompleted} {
			t.Run(string(mode)+"/"+string(sprint), func(t *testing.T) {
				root := t.TempDir()
				path, _ := testhelpers.SetupLizaDir(t, root)
				state := testhelpers.CreateValidState()
				state.Config.Mode = mode
				state.Sprint.Status = sprint
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("preserved", models.TaskStatusBlocked, time.Now().UTC())}
				testhelpers.WriteInitialState(t, path, state)
				before, err := db.For(path).Read()
				if err != nil {
					t.Fatal(err)
				}
				for _, enabled := range []bool{true, true, false, false} {
					previous, err := db.For(path).Read()
					if err != nil {
						t.Fatal(err)
					}
					result, err := SetAutoResumeWithResult(root, enabled)
					if err != nil {
						t.Fatal(err)
					}
					want := "set"
					if previous.Config.AutoResume == enabled {
						want = "unchanged"
					}
					if result.Key != AutoResumeConfigKey || result.Value != enabled || result.Outcome != want {
						t.Fatalf("wrong result: %+v", result)
					}
					after, err := db.For(path).Read()
					if err != nil {
						t.Fatal(err)
					}
					if after.Config.AutoResume != enabled {
						t.Fatal("result differs from committed value")
					}
					if !reflect.DeepEqual(autoResumeLogicalState(t, before), autoResumeLogicalState(t, after)) {
						t.Fatal("policy write changed execution state")
					}
				}
			})
		}
	}
}

func TestSetAutoResumeWithResultConcurrentNativeWrites(t *testing.T) {
	root := t.TempDir()
	path, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	original := state.Goal.Description
	testhelpers.WriteInitialState(t, path, state)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(enabled bool) {
			defer wg.Done()
			if _, err := SetAutoResumeWithResult(root, enabled); err != nil {
				t.Error(err)
			}
		}(i%2 == 0)
		go func() {
			defer wg.Done()
			if err := db.For(path).Modify(func(s *models.State) error { s.Goal.Description += "."; return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	after, err := db.For(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Goal.Description) != len(original)+20 {
		t.Fatal("policy setter lost a concurrent native write")
	}
}

func TestSetAutoResumeWithResultFailsClosed(t *testing.T) {
	if result, err := SetAutoResumeWithResult(t.TempDir(), true); err == nil || result != nil {
		t.Fatal("missing workspace reported success")
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions cannot enforce a write failure on this platform/user")
	}
	root := t.TempDir()
	path, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.WriteInitialState(t, path, testhelpers.CreateValidState())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0755) })
	if result, err := SetAutoResumeWithResult(root, true); err == nil || result != nil {
		t.Fatal("failed persistence reported success")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed policy write changed saved state")
	}
}
