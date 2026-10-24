package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

const (
	channelStable = "stable"
	channelMain   = "main"

	channelEnvSuffix = "UPDATE_CHANNEL"
	checkEnvSuffix   = "CHECK_UPDATE"
	skipEnvSuffix    = "SKIP_AUTO_UPDATE"
	updatePrefs      = "update.json"
)

func checkUpdateFlag(args []string) (bool, bool) {
	flags, err := parseUpdateFlags(args)
	if err != nil {
		return false, false
	}
	if flags.checkUpdate != nil {
		return *flags.checkUpdate, true
	}
	return false, false
}

func checkUpdateEnvEnabled(cfg Config) bool {
	switch strings.ToLower(strings.TrimSpace(envValueForSuffix(cfg, checkEnvSuffix))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type preferences struct {
	CheckUpdate *bool  `json:"check_update,omitempty"`
	Channel     string `json:"channel,omitempty"`
}

type parsedUpdateFlags struct {
	checkUpdate  *bool
	channel      string
	settingsOnly bool
}

func updatePrefsPath() (string, error) {
	dir, err := paths.GlobalLizaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, updatePrefs), nil
}

func UpdateChecksDisabled() bool {
	prefs := readUpdatePreferences()
	return prefs.CheckUpdate != nil && !*prefs.CheckUpdate
}

func readUpdatePreferences() preferences {
	path, err := updatePrefsPath()
	if err != nil {
		return preferences{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return preferences{}
	}
	var prefs preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return preferences{}
	}
	prefs.Channel = strings.ToLower(strings.TrimSpace(prefs.Channel))
	return prefs
}

func DisableUpdateChecks() error {
	prefs := readUpdatePreferences()
	disabled := false
	prefs.CheckUpdate = &disabled
	return writeUpdatePreferences(prefs)
}

func persistExplicitUpdatePreferences(cfg Config) error {
	var changed bool
	prefs := readUpdatePreferences()

	if enabled, ok := checkUpdateFlag(cfg.Args); ok {
		prefs.CheckUpdate = &enabled
		changed = true
	}
	if channel, ok := updateChannelFlag(cfg.Args); ok {
		if err := validateChannel(channel); err != nil {
			return err
		}
		prefs.Channel = channel
		changed = true
	}
	if !changed {
		return nil
	}
	if err := writeUpdatePreferences(prefs); err != nil {
		return &FatalError{fmt.Errorf("write update preferences: %w", err)}
	}
	return nil
}

func validateExplicitUpdateFlags(args []string) error {
	_, err := parseUpdateFlags(args)
	return err
}

func UpdateSettingsOnly(args []string) bool {
	flags, err := parseUpdateFlags(args)
	return err == nil && flags.settingsOnly
}

func parseUpdateFlags(args []string) (parsedUpdateFlags, error) {
	var flags parsedUpdateFlags
	changed := false
	settingsOnly := true
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			flags.settingsOnly = changed && settingsOnly && i == len(args)-1
			return flags, nil
		case arg == "--check-update":
			flags.checkUpdate = boolPtr(true)
		case strings.HasPrefix(arg, "--check-update="):
			value := strings.TrimPrefix(arg, "--check-update=")
			if value != "true" && value != "false" {
				return flags, &FatalError{fmt.Errorf("invalid --check-update value %q (valid values: true, false)", value)}
			}
			flags.checkUpdate = boolPtr(value == "true")
		case arg == "--update-channel":
			if i+1 >= len(args) || args[i+1] == "--" {
				return flags, &FatalError{fmt.Errorf("--update-channel requires a value (valid values: stable, main)")}
			}
			channel := strings.ToLower(strings.TrimSpace(args[i+1]))
			flags.channel = channel
			if err := validateChannel(channel); err != nil {
				return flags, err
			}
			i++
		case strings.HasPrefix(arg, "--update-channel="):
			channel := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--update-channel=")))
			flags.channel = channel
			if err := validateChannel(channel); err != nil {
				return flags, err
			}
		default:
			settingsOnly = false
			continue
		}
		changed = true
	}
	flags.settingsOnly = changed && settingsOnly
	return flags, nil
}

func boolPtr(value bool) *bool {
	return &value
}

func SavedUpdateSettingsSummary() string {
	prefs := readUpdatePreferences()
	checkUpdate := "unset"
	if prefs.CheckUpdate != nil {
		checkUpdate = fmt.Sprintf("%t", *prefs.CheckUpdate)
	}
	channel := prefs.Channel
	if channel == "" {
		channel = channelStable
	}
	return fmt.Sprintf("Update settings saved: check_update=%s, channel=%s\n", checkUpdate, channel)
}

func writeUpdatePreferences(prefs preferences) error {
	path, err := updatePrefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create global %s config dir: %w", brand.NameTitle, err)
	}
	prefs.Channel = strings.ToLower(strings.TrimSpace(prefs.Channel))
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update preferences: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write update preferences: %w", err)
	}
	return nil
}

func validateChannel(channel string) error {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != channelStable && channel != channelMain {
		return &FatalError{fmt.Errorf("invalid update channel %q (valid values: stable, main)", channel)}
	}
	return nil
}

func updateChannel(cfg Config) string {
	if channel, ok := updateChannelFlag(cfg.Args); ok {
		return channel
	}
	if cfg.Channel != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Channel))
	}
	if envChannel := envValueForSuffix(cfg, channelEnvSuffix); envChannel != "" {
		return strings.ToLower(strings.TrimSpace(envChannel))
	}
	if prefs := readUpdatePreferences(); prefs.Channel != "" {
		return prefs.Channel
	}
	return channelStable
}

func updateChannelFlag(args []string) (string, bool) {
	flags, err := parseUpdateFlags(args)
	if err != nil {
		return "", false
	}
	if flags.channel != "" {
		return flags.channel, true
	}
	return "", false
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			out = append(out, prefix+value)
			replaced = true
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

func envValueForSuffix(cfg Config, suffix string) string {
	lookup := brand.RuntimeValues().LookupEnv(func(key string) string {
		return envValue(cfg.Env, key)
	}, suffix)
	if lookup.Warning != "" && cfg.Stderr != nil {
		fmt.Fprintf(cfg.Stderr, "Warning: %s\n", lookup.Warning)
	}
	return lookup.Value
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
