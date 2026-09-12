package pipeline

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestLoadFrozenMigratesReconcileVerdictWithoutRewritingFile(t *testing.T) {
	for _, namedOrchestratorPresent := range []bool{true, false} {
		name := "existing named orchestrator"
		if !namedOrchestratorPresent {
			name = "absent named orchestrator"
		}
		t.Run(name, func(t *testing.T) {
			legacy := frozenConfigWithout(t, "reconcile-verdict")
			orchestrator, ok := legacy.Pipeline.Roles["orchestrator"]
			if !ok {
				t.Fatal("reference lacks orchestrator role")
			}
			orchestrator.AllowedOperations = []string{"custom-operation"}
			legacy.Pipeline.Roles["custom-orchestrator"] = orchestrator
			if !namedOrchestratorPresent {
				delete(legacy.Pipeline.Roles, "orchestrator")
			}
			data, err := yaml.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			testhelpers.SetupPipelineConfigBytes(t, root, data)
			frozenPath := filepath.Join(paths.New(root).LizaDir(), "pipeline.yaml")

			for load := 0; load < 2; load++ {
				cfg, err := LoadFrozen(root)
				if err != nil {
					t.Fatalf("LoadFrozen: %v", err)
				}
				if len(cfg.Pipeline.Roles) != len(legacy.Pipeline.Roles) {
					t.Fatalf("loading synthesized or removed roles: got %d, want %d", len(cfg.Pipeline.Roles), len(legacy.Pipeline.Roles))
				}
				resolver := NewResolver(cfg)
				for roleName := range cfg.Pipeline.Roles {
					capabilities, err := resolver.EffectiveRoleCapabilities(roleName)
					if err != nil {
						t.Fatalf("capabilities for %s: %v", roleName, err)
					}
					want := roleName == "orchestrator" && namedOrchestratorPresent
					if got := capabilities.Allows("reconcile-verdict"); got != want {
						t.Errorf("role %s reconciliation capability = %v, want %v", roleName, got, want)
					}
					if want {
						count := 0
						for _, operation := range capabilities.AllowedOperations {
							if operation == "reconcile-verdict" {
								count++
							}
						}
						if count != 1 {
							t.Errorf("reconcile-verdict appears %d times", count)
						}
					}
				}
				if !slices.Equal(cfg.Pipeline.Roles["custom-orchestrator"].AllowedOperations, []string{"custom-operation"}) {
					t.Fatal("custom orchestrator inherited capabilities by role type")
				}
				after, err := os.ReadFile(frozenPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, data) {
					t.Fatal("LoadFrozen rewrote the frozen pipeline YAML")
				}
			}
		})
	}
}
