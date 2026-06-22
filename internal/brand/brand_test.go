package brand

import (
	"strings"
	"testing"
)

func TestValuesFromEnvDerivedDefaults(t *testing.T) {
	values := ValuesFromEnv(func(key string) string {
		if key == "BRAND_NAME_LOWER" {
			return "acme-agent"
		}
		if key == "BRAND_REPO" {
			return "acme/agent"
		}
		return ""
	})
	if values.BinaryName != "acme-agent" {
		t.Fatalf("BinaryName = %q, want derived lower name", values.BinaryName)
	}
	if values.GlobalDirName != ".acme-agent" || values.ProjectDirName != ".acme-agent" {
		t.Fatalf("derived dirs = %q/%q", values.GlobalDirName, values.ProjectDirName)
	}
	if values.EnvPrefix != "ACME_AGENT" {
		t.Fatalf("EnvPrefix = %q", values.EnvPrefix)
	}
	if values.NameTitle != "Acme Agent" {
		t.Fatalf("NameTitle = %q", values.NameTitle)
	}
	if values.ArchivePrefix != values.BinaryName {
		t.Fatalf("ArchivePrefix = %q, want binary name", values.ArchivePrefix)
	}
	if values.ReleaseBaseURL != "https://github.com/acme/agent/releases/download" {
		t.Fatalf("ReleaseBaseURL = %q", values.ReleaseBaseURL)
	}
	if err := Validate(values); err != nil {
		t.Fatalf("Validate derived values: %v", err)
	}
}

func TestRuntimeValuesUseCanonicalMistralPromptID(t *testing.T) {
	values := RuntimeValues()
	if values.MistralPromptID != CanonicalMistralPromptID {
		t.Fatalf("MistralPromptID = %q, want canonical %q", values.MistralPromptID, CanonicalMistralPromptID)
	}
}

func TestValidateRejectsUnsafeTitle(t *testing.T) {
	values := RuntimeValues()
	values.NameTitle = `Bad "Title"`
	err := Validate(values)
	if err == nil || !strings.Contains(err.Error(), "BRAND_NAME_TITLE") {
		t.Fatalf("Validate error = %v, want BRAND_NAME_TITLE rejection", err)
	}
}

func TestValidateRejectsUnsafeURL(t *testing.T) {
	values := RuntimeValues()
	values.ReleaseBaseURL = "https://example.com/releases/`bad`"
	err := Validate(values)
	if err == nil || !strings.Contains(err.Error(), "BRAND_RELEASE_BASE_URL") {
		t.Fatalf("Validate error = %v, want BRAND_RELEASE_BASE_URL rejection", err)
	}
}

func TestLookupEnvBrandedWinsAndWarns(t *testing.T) {
	values := RuntimeValues()
	values.EnvPrefix = "ACME_AGENT"
	got := values.LookupEnv(func(key string) string {
		switch key {
		case "ACME_AGENT_AGENT_ID":
			return "new"
		case "LIZA_AGENT_ID":
			return "old"
		default:
			return ""
		}
	}, "AGENT_ID")
	if got.Value != "new" || got.Source != "ACME_AGENT_AGENT_ID" {
		t.Fatalf("LookupEnv = %+v, want branded source/value", got)
	}
	if !strings.Contains(got.Warning, "ACME_AGENT_AGENT_ID") || !strings.Contains(got.Warning, "LIZA_AGENT_ID") {
		t.Fatalf("warning = %q, want mixed env warning", got.Warning)
	}
}

func TestLookupEnvLegacyAlias(t *testing.T) {
	values := RuntimeValues()
	values.EnvPrefix = "ACME_AGENT"
	got := values.LookupEnv(func(key string) string {
		if key == "LIZA_AGENT_ID" {
			return "legacy"
		}
		return ""
	}, "AGENT_ID")
	if got.Value != "legacy" || got.Source != "LIZA_AGENT_ID" || got.Warning != "" {
		t.Fatalf("LookupEnv = %+v, want legacy alias without warning", got)
	}
}
