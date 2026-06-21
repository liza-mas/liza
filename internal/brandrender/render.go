package brandrender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/pelletier/go-toml/v2"
)

var macroRE = regexp.MustCompile(`§BRAND_[A-Z0-9_]+§`)

type SyncOptions struct {
	RepoRoot string
	Values   brand.Values
}

type RenderedFile struct {
	RelPath string
	Content []byte
	Mode    fs.FileMode
}

func SyncEmbedded(opts SyncOptions) error {
	repoRoot, values, err := normalizeOptions(opts)
	if err != nil {
		return err
	}
	files, err := ExpectedEmbeddedFiles(repoRoot, values)
	if err != nil {
		return err
	}
	embeddedRoot := filepath.Join(repoRoot, "internal", "embedded")
	for _, rel := range []string{"contracts", "skills", "support-docs", "docs", "specs"} {
		if err := os.RemoveAll(filepath.Join(embeddedRoot, rel)); err != nil {
			return fmt.Errorf("remove embedded %s: %w", rel, err)
		}
	}
	for _, file := range files {
		target := filepath.Join(embeddedRoot, filepath.FromSlash(file.RelPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", target, err)
		}
		mode := file.Mode.Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(target, file.Content, mode); err != nil {
			return fmt.Errorf("write rendered embedded file %s: %w", target, err)
		}
	}
	return nil
}

func ExpectedEmbeddedFiles(repoRoot string, values brand.Values) ([]RenderedFile, error) {
	repoRoot, values, err := normalizeOptions(SyncOptions{RepoRoot: repoRoot, Values: values})
	if err != nil {
		return nil, err
	}
	var out []RenderedFile
	if err := appendTopLevelMarkdown(&out, repoRoot, values, "contracts", "contracts"); err != nil {
		return nil, err
	}
	if err := appendTree(&out, repoRoot, values, "skills", "skills"); err != nil {
		return nil, err
	}
	if err := appendTopLevelMarkdown(&out, repoRoot, values, "support-docs", "support-docs"); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeOptions(opts SyncOptions) (string, brand.Values, error) {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return "", brand.Values{}, fmt.Errorf("get working directory: %w", err)
		}
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", brand.Values{}, fmt.Errorf("resolve repository root: %w", err)
	}
	values := opts.Values
	if values.NameLower == "" {
		values = brand.ValuesFromEnv(os.Getenv)
	}
	if err := brand.Validate(values); err != nil {
		return "", brand.Values{}, err
	}
	return absRoot, brand.Normalize(values), nil
}

func appendTopLevelMarkdown(out *[]RenderedFile, repoRoot string, values brand.Values, srcRel, dstRel string) error {
	entries, err := os.ReadDir(filepath.Join(repoRoot, srcRel))
	if err != nil {
		return fmt.Errorf("read %s: %w", srcRel, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		src := filepath.Join(repoRoot, srcRel, entry.Name())
		dst := filepath.ToSlash(filepath.Join(dstRel, entry.Name()))
		file, err := renderSourceFile(src, dst, values)
		if err != nil {
			return err
		}
		*out = append(*out, file)
	}
	return nil
}

func appendTree(out *[]RenderedFile, repoRoot string, values brand.Values, srcRel, dstRel string) error {
	srcRoot := filepath.Join(repoRoot, srcRel)
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		dst := filepath.ToSlash(filepath.Join(dstRel, RenderPath(rel, values)))
		file, err := renderSourceFile(path, dst, values)
		if err != nil {
			return err
		}
		*out = append(*out, file)
		return nil
	})
}

func renderSourceFile(srcPath, dstRel string, values brand.Values) (RenderedFile, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("read %s: %w", srcPath, err)
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("stat %s: %w", srcPath, err)
	}
	rendered, err := RenderBytes(content, values)
	if err != nil {
		return RenderedFile{}, fmt.Errorf("render %s: %w", srcPath, err)
	}
	if err := ValidateRenderedFile(dstRel, rendered); err != nil {
		return RenderedFile{}, fmt.Errorf("validate %s: %w", srcPath, err)
	}
	return RenderedFile{RelPath: dstRel, Content: rendered, Mode: info.Mode()}, nil
}

func RenderBytes(content []byte, values brand.Values) ([]byte, error) {
	if err := brand.Validate(values); err != nil {
		return nil, err
	}
	macros := brand.MacroMap(values)
	rendered := macroRE.ReplaceAllFunc(content, func(token []byte) []byte {
		name := strings.TrimSuffix(strings.TrimPrefix(string(token), "§"), "§")
		value, ok := macros[name]
		if !ok {
			return token
		}
		return []byte(value)
	})
	unknown := macroRE.Find(rendered)
	if unknown != nil {
		return nil, fmt.Errorf("unknown brand macro %s", unknown)
	}
	if bytes.ContainsRune(rendered, '§') {
		return nil, fmt.Errorf("stray brand macro delimiter § outside declared token")
	}
	return rendered, nil
}

func RenderPath(rel string, values brand.Values) string {
	values = valuesFromDefaults(values)
	rel = filepath.ToSlash(rel)
	replacements := []struct {
		old string
		new string
	}{
		{"liza-logs", values.NameLower + "-logs"},
		{"liza-index", values.BinaryName + "-index"},
		{"liza-session", values.BinaryName + "-session"},
		{".liza-hooks", values.ProjectDirName + "-hooks"},
	}
	for _, replacement := range replacements {
		rel = strings.ReplaceAll(rel, replacement.old, replacement.new)
	}
	return rel
}

func valuesFromDefaults(values brand.Values) brand.Values {
	if values.NameLower == "" {
		return brand.RuntimeValues()
	}
	return brand.Normalize(values)
}

func ValidateRenderedFile(relPath string, content []byte) error {
	slash := filepath.ToSlash(relPath)
	switch {
	case strings.HasSuffix(slash, ".json"):
		var decoded any
		if err := json.Unmarshal(content, &decoded); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
	case strings.HasSuffix(slash, ".toml"):
		var decoded map[string]any
		if err := toml.Unmarshal(content, &decoded); err != nil {
			return fmt.Errorf("invalid TOML: %w", err)
		}
	case strings.HasSuffix(slash, ".sh"):
		if err := validateBashSyntax(content); err != nil {
			return err
		}
	}
	if macroRE.Find(content) != nil {
		return fmt.Errorf("unresolved brand macro in rendered file")
	}
	if bytes.ContainsRune(content, '§') {
		return fmt.Errorf("stray brand macro delimiter § in rendered file")
	}
	return nil
}

func validateBashSyntax(content []byte) error {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return nil
	}
	tmp, err := os.CreateTemp("", "brandrender-*.sh")
	if err != nil {
		return fmt.Errorf("create shell syntax temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write shell syntax temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close shell syntax temp file: %w", err)
	}
	cmd := exec.Command(bashPath, "-n", tmpPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("invalid shell syntax: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
