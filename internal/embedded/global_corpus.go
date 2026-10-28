package embedded

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

type embeddedCorpus struct {
	name    string
	fs      embed.FS
	destDir string
}

func globalCorpora(targetDir string) []embeddedCorpus {
	return []embeddedCorpus{
		{name: "contracts", fs: contractsFS, destDir: targetDir},
		{name: "skills", fs: skillsFS, destDir: filepath.Join(targetDir, "skills")},
		{name: "support docs", fs: supportDocsFS, destDir: filepath.Join(targetDir, "support-docs")},
	}
}

// PlanGlobalFiles returns the list of absolute paths that WriteGlobalFiles would
// create, without actually writing anything. Useful for pre-flight checks and
// verbose output.
func PlanGlobalFiles(targetDir string) []string {
	var paths []string
	for _, pair := range globalCorpora(targetDir) {
		_ = fs.WalkDir(pair.fs, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || path == "." {
				return nil
			}
			parts := strings.Split(path, "/")
			if len(parts) <= 1 {
				return nil
			}
			paths = append(paths, filepath.Join(pair.destDir, filepath.Join(parts[1:]...)))
			return nil
		})
	}
	return paths
}

// WriteGlobalFiles writes contracts, skills, and support docs to the global
// runtime directory. Contracts are written flat into targetDir/,
// skills into targetDir/skills/, and support docs into
// targetDir/support-docs/. Markdown files are prepended with YAML frontmatter
// containing version metadata. Files whose absolute path appears in skipFiles
// are silently skipped. Returns the list of absolute paths written.
func WriteGlobalFiles(targetDir string, skipFiles map[string]bool) ([]string, error) {
	var written []string
	for _, corpus := range globalCorpora(targetDir) {
		paths, err := writeEmbeddedFS(corpus.fs, corpus.destDir, skipFiles)
		if err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", corpus.name, err)
		}
		written = append(written, paths...)
	}

	return written, nil
}

// writeEmbeddedFS writes an entire embedded filesystem to the target directory.
// Files whose absolute path appears in skipFiles are silently skipped.
// Returns the list of absolute paths of files written (directories excluded).
func writeEmbeddedFS(embeddedFS embed.FS, targetDir string, skipFiles map[string]bool) ([]string, error) {
	var written []string
	err := fs.WalkDir(embeddedFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == "." {
			return nil
		}

		// embed.FS paths include the top-level dir (e.g. "contracts/CORE.md"),
		// but targetDir already points there — strip the first component.
		parts := strings.Split(path, "/")
		if len(parts) == 1 {
			return nil
		}
		relativePath := filepath.Join(parts[1:]...)
		targetPath := filepath.Join(targetDir, relativePath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		if skipFiles[targetPath] {
			return nil
		}

		content, err := embeddedFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		// Only prepend version frontmatter to Markdown files;
		// scripts and other artifacts are written verbatim.
		output := content
		if strings.HasSuffix(relativePath, ".md") {
			output = PrependFrontmatter(content)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", targetPath, err)
		}

		if err := os.WriteFile(targetPath, output, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", targetPath, err)
		}

		written = append(written, targetPath)
		return nil
	})
	return written, err
}

// PrependFrontmatter merges version metadata into existing YAML frontmatter,
// or prepends a new frontmatter block if none exists.
// Existing non-brand fields (e.g., skill name/description) are preserved;
// old brand metadata fields are replaced with current build values.
func PrependFrontmatter(content []byte) []byte {
	prefix := []byte("---\n")
	if !bytes.HasPrefix(content, prefix) {
		return append([]byte(frontmatter()), content...)
	}

	rest := content[len(prefix):]
	existingBlockBytes, after, found := bytes.Cut(rest, []byte("\n---\n"))
	if !found {
		return append([]byte(frontmatter()), content...) // malformed, prepend fresh
	}

	existingBlock := string(existingBlockBytes)

	var kept []string
	metadataPrefix := brand.MetadataPrefix() + "_"
	for line := range strings.SplitSeq(existingBlock, "\n") {
		if !strings.HasPrefix(line, "liza_") && !strings.HasPrefix(line, metadataPrefix) {
			kept = append(kept, line)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	for _, line := range kept {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	buf.WriteString(versionFields())
	buf.WriteString("---\n")

	// Preserve blank line between frontmatter and body
	if len(after) > 0 && after[0] != '\n' {
		buf.WriteByte('\n')
	}
	buf.Write(after)

	return buf.Bytes()
}

// stripFrontmatter removes a leading YAML frontmatter block ("---\n...---\n") if present.
func stripFrontmatter(content []byte) []byte {
	prefix := []byte("---\n")
	if !bytes.HasPrefix(content, prefix) {
		return content
	}
	rest := content[len(prefix):]
	_, after, found := bytes.Cut(rest, []byte("\n---\n"))
	if !found {
		return content // malformed frontmatter, leave as-is
	}
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	return after
}

// versionFields returns the brand-prefixed key-value lines without YAML delimiters.
func versionFields() string {
	return fmt.Sprintf("%s: \"%s\"\n%s: \"%s\"\n%s: \"%s\"\n",
		brand.MetadataKey("version"), Version,
		brand.MetadataKey("git_commit"), GitCommit,
		brand.MetadataKey("build_date"), BuildDate)
}

// frontmatter generates a complete YAML frontmatter block from build-time variables.
func frontmatter() string {
	return "---\n" + versionFields() + "---\n\n"
}

// ListEmbeddedFiles returns a list of all embedded file paths (for testing).
func ListEmbeddedFiles() ([]string, error) {
	var files []string
	for _, fsys := range []embed.FS{contractsFS, skillsFS, supportDocsFS} {
		collected, err := collectFiles(fsys)
		if err != nil {
			return nil, err
		}
		files = append(files, collected...)
	}
	return files, nil
}

// collectFiles returns all file paths from an embedded filesystem.
func collectFiles(fsys embed.FS) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path != "." {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
