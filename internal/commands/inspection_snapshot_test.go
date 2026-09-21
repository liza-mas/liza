package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func inspectionSnapshotFixture(t testing.TB, taskCount int) (string, *models.State, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	p := paths.New(root)
	if err := os.MkdirAll(p.LizaDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := embedded.WritePipelineConfig(p.LizaDir(), nil); err != nil {
		t.Fatal(err)
	}
	state := testhelpers.CreateValidState()
	for i := range taskCount {
		state.Tasks = append(state.Tasks, models.Task{
			ID: fmt.Sprintf("task-%d", i), Status: "US_APPROVED", RolePair: "us-writing-pair",
			Description: strings.Repeat("task description ", 32), Created: time.Now().UTC(),
		})
	}
	state.Agents["supervisor-1"] = models.Agent{Role: "coder", Heartbeat: time.Now().UTC()}
	bb := db.New(p.StatePath())
	if err := bb.Write(state); err != nil {
		t.Fatal(err)
	}
	return root, state, bb
}

func TestInspectionSnapshotHeldLock(t *testing.T) {
	for _, noFollowUp := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-follow-up=%v", noFollowUp), func(t *testing.T) {
			root, state, bb := inspectionSnapshotFixture(t, 1)
			state.Config.NoFollowUp = noFollowUp
			if err := bb.Write(state); err != nil {
				t.Fatal(err)
			}
			reader := db.For(bb.GetStatePath())
			reader.EnableMetrics()
			defer reader.DisableMetrics()
			err := filelock.New(bb.GetStatePath()).WithLockOperation("independent writer", func() error {
				done := make(chan error, 1)
				go func() {
					for _, format := range []string{"", "json", "yaml"} {
						output, err := StatusCommand(StatusOptions{ProjectRoot: root, Format: format})
						if err != nil {
							done <- err
							return
						}
						if format == "json" {
							var data statusData
							if err := json.Unmarshal([]byte(output), &data); err != nil {
								done <- err
								return
							}
							if (len(data.PendingTransitions) == 0) != noFollowUp {
								done <- fmt.Errorf("pending transitions disagree with snapshot no_follow_up=%v: %v", noFollowUp, data.PendingTransitions)
								return
							}
						}
						if _, err := InspectCommand([]string{"tasks"}, InspectOptions{ProjectRoot: root, Format: format}); err != nil {
							done <- err
							return
						}
					}
					done <- nil
				}()
				select {
				case err := <-done:
					return err
				case <-time.After(5 * time.Second):
					return fmt.Errorf("inspection blocked behind state lock")
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if n := len(reader.GetMetricsRecorder().GetMetrics()); n != 0 {
				t.Fatalf("inspection acquired state lock %d times", n)
			}
			// A caller-supplied snapshot owns the policy, even when disk differs.
			state.Config.NoFollowUp = !noFollowUp
			data := BuildStatusData(state, false, root, nil, nil)
			if (len(data.PendingTransitions) == 0) != state.Config.NoFollowUp {
				t.Fatal("status reread runtime policy instead of using the supplied snapshot")
			}
		})
	}
}

// This benchmark runs full status/get formatting while an independent writer
// renews supervisor heartbeat state. It reports workload timings, not a brittle
// wall-clock assertion; held-lock tests establish the nonblocking contract.
func BenchmarkInspectionSnapshotDuringHeartbeat(b *testing.B) {
	for _, command := range []string{"status", "get"} {
		b.Run(command, func(b *testing.B) {
			root, _, writer := inspectionSnapshotFixture(b, 100)
			writer = writer.WithLockTimeout(time.Second)
			stop := make(chan struct{})
			type writerResult struct {
				count int
				max   time.Duration
				err   error
			}
			done := make(chan writerResult, 1)
			go func() {
				result := writerResult{}
				for {
					select {
					case <-stop:
						done <- result
						return
					default:
					}
					start := time.Now()
					err := writer.Modify(func(state *models.State) error {
						agent := state.Agents["supervisor-1"]
						agent.Heartbeat = time.Now().UTC()
						state.Agents["supervisor-1"] = agent
						return nil
					})
					if err != nil {
						result.err = err
						done <- result
						return
					}
					result.count++
					result.max = max(result.max, time.Since(start))
				}
			}()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var err error
					if command == "status" {
						_, err = StatusCommand(StatusOptions{ProjectRoot: root, Format: "json"})
					} else {
						_, err = InspectCommand([]string{"tasks"}, InspectOptions{ProjectRoot: root, Format: "json"})
					}
					if err != nil {
						b.Error(err)
						return
					}
				}
			})
			b.StopTimer()
			close(stop)
			result := <-done
			if result.err != nil {
				b.Fatal(result.err)
			}
			b.ReportMetric(float64(result.count), "heartbeat-writes")
			b.ReportMetric(float64(result.max.Microseconds()), "max-heartbeat-us")
		})
	}
}
