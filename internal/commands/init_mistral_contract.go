package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/termutil"
)

// mistralSystemPromptKey is the config.toml key that selects Mistral's system prompt.
const mistralSystemPromptKey = "system_prompt_id"

// setupMistralContract creates a Mistral prompt symlink to CORE.md and sets system_prompt_id in config.toml.
func setupMistralContract(coreFile string, reader *bufio.Reader, autoConfirm bool) error {
	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	vibeDir := filepath.Join(homeDir, mistralConfigDirName)
	promptsDir := filepath.Join(vibeDir, mistralPromptsDirName)
	if err := os.MkdirAll(promptsDir, initDirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", promptsDir, err)
	}

	promptID := brand.CanonicalMistralPromptID

	// Create prompts/<prompt-id>.md symlink (with confirmation for overwrites)
	linkPath := filepath.Join(promptsDir, promptID+".md")
	if err := createSymlinkIdempotent(coreFile, linkPath, reader, true, autoConfirm); err != nil {
		return fmt.Errorf("failed to create %s.md symlink: %w", promptID, err)
	}

	// Update config.toml with the canonical provider prompt ID.
	configPath := filepath.Join(vibeDir, mistralConfigFileName)
	if err := setMistralSystemPrompt(configPath, reader, promptID, autoConfirm); err != nil {
		return err
	}

	return nil
}

// setMistralSystemPrompt ensures system_prompt_id is set in ~/.vibe/config.toml.
// Prompts user before modifying an existing file.
func setMistralSystemPrompt(configPath string, reader *bufio.Reader, promptID string, autoConfirm bool) error {
	promptLine := fmt.Sprintf("%s = %q", mistralSystemPromptKey, promptID)
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new config with just the prompt ID — no confirmation needed
			if err := os.WriteFile(configPath, []byte(promptLine+"\n"), initFileMode); err != nil {
				return fmt.Errorf("failed to create %s: %w", configPath, err)
			}
			fmt.Printf("Created %s with %s\n", configPath, promptLine)
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	text := string(content)

	// Already set correctly
	if strings.Contains(text, promptLine) {
		fmt.Printf("%s: system_prompt_id already set to %q\n", configPath, promptID)
		return nil
	}

	// Needs modification — ask user
	fmt.Fprintf(os.Stderr, "%s exists and system_prompt_id is not set to %q.\n", configPath, promptID)
	fmt.Fprintf(os.Stderr, "Set system_prompt_id = %q? (y/n): ", promptID)
	if autoConfirm {
		fmt.Fprintln(os.Stderr, "yes")
	} else {
		response, err := termutil.ReadSingleKey(reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read input, skipping config.toml update\n")
			return nil
		}
		if response != "y" {
			fmt.Fprintln(os.Stderr) // Print newline for clean terminal output
			fmt.Fprintf(os.Stderr, "  Skipped %s\n", configPath)
			return nil
		}
	}

	// Replace existing system_prompt_id line
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, mistralSystemPromptKey) && strings.Contains(trimmed, "=") {
			lines[i] = promptLine
			found = true
			break
		}
	}

	if !found {
		// Prepend to file
		lines = append([]string{promptLine}, lines...)
	}

	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), initFileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	fmt.Printf("%s: set system_prompt_id = %q\n", configPath, promptID)
	return nil
}
