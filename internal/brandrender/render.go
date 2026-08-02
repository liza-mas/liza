package brandrender

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
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

var managedEmbeddedDirs = []string{"contracts", "skills", "support-docs", "docs", "specs"}

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
	desired, desiredDirs, err := preflightRenderedFiles(files)
	if err != nil {
		return err
	}
	embeddedRoot := filepath.Join(repoRoot, "internal", "embedded")
	rootInfo, err := os.Lstat(embeddedRoot)
	if err != nil {
		return fmt.Errorf("inspect embedded root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("embedded root must be a non-symlink directory: %s", embeddedRoot)
	}
	root, err := os.OpenRoot(embeddedRoot)
	if err != nil {
		return fmt.Errorf("open embedded root: %w", err)
	}
	defer root.Close()

	if err := pruneManagedEmbedded(root, desired, desiredDirs); err != nil {
		return err
	}
	for _, file := range files {
		if err := syncRenderedFile(root, file); err != nil {
			return err
		}
	}
	return nil
}

func ManagedEmbeddedDirs() []string {
	return append([]string(nil), managedEmbeddedDirs...)
}

func preflightRenderedFiles(files []RenderedFile) (map[string]RenderedFile, map[string]bool, error) {
	desired := make(map[string]RenderedFile, len(files))
	desiredDirs := make(map[string]bool)
	for _, file := range files {
		rel := filepath.ToSlash(file.RelPath)
		if rel == "." || !fs.ValidPath(rel) {
			return nil, nil, fmt.Errorf("invalid rendered path %q", file.RelPath)
		}
		if _, exists := desired[rel]; exists {
			return nil, nil, fmt.Errorf("duplicate rendered path %q", rel)
		}
		if desiredDirs[rel] {
			return nil, nil, fmt.Errorf("rendered path collision: file %q is also a required directory", rel)
		}
		file.RelPath = rel
		file.Mode = renderedMode(file.Mode)
		desired[rel] = file
		for dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel))); dir != "."; dir = filepath.ToSlash(filepath.Dir(filepath.FromSlash(dir))) {
			if _, exists := desired[dir]; exists {
				return nil, nil, fmt.Errorf("rendered path collision: directory %q is also a rendered file", dir)
			}
			desiredDirs[dir] = true
		}
	}
	return desired, desiredDirs, nil
}

func pruneManagedEmbedded(root *os.Root, desired map[string]RenderedFile, desiredDirs map[string]bool) error {
	for _, managedDir := range managedEmbeddedDirs {
		info, err := root.Lstat(managedDir)
		switch {
		case errorsIsNotExist(err):
			continue
		case err != nil:
			return fmt.Errorf("inspect embedded %s: %w", managedDir, err)
		case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
			if err := root.RemoveAll(managedDir); err != nil {
				return fmt.Errorf("remove non-directory embedded %s: %w", managedDir, err)
			}
			continue
		}

		var paths []string
		if err := fs.WalkDir(root.FS(), managedDir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != managedDir {
				paths = append(paths, filepath.ToSlash(path))
			}
			return nil
		}); err != nil {
			return fmt.Errorf("walk embedded %s: %w", managedDir, err)
		}
		for i := len(paths) - 1; i >= 0; i-- {
			rel := paths[i]
			if _, keepFile := desired[rel]; keepFile || desiredDirs[rel] {
				continue
			}
			if err := root.RemoveAll(filepath.FromSlash(rel)); err != nil {
				return fmt.Errorf("remove stale embedded %s: %w", rel, err)
			}
		}
		if !desiredDirs[managedDir] {
			if err := root.RemoveAll(managedDir); err != nil {
				return fmt.Errorf("remove stale embedded %s: %w", managedDir, err)
			}
		}
	}
	return nil
}

func syncRenderedFile(root *os.Root, file RenderedFile) error {
	rel := filepath.FromSlash(file.RelPath)
	if err := ensureRootDirs(root, filepath.Dir(rel)); err != nil {
		return fmt.Errorf("prepare directory for %s: %w", file.RelPath, err)
	}

	info, err := root.Lstat(rel)
	if errorsIsNotExist(err) {
		return writeRootFileAtomic(root, rel, file.Content, renderedMode(file.Mode))
	}
	if err != nil {
		return fmt.Errorf("inspect rendered embedded file %s: %w", file.RelPath, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		if err := root.RemoveAll(rel); err != nil {
			return fmt.Errorf("remove non-regular embedded target %s: %w", file.RelPath, err)
		}
		return writeRootFileAtomic(root, rel, file.Content, renderedMode(file.Mode))
	}

	current, err := root.ReadFile(rel)
	if err != nil {
		return fmt.Errorf("read rendered embedded file %s: %w", file.RelPath, err)
	}
	if !bytes.Equal(current, file.Content) {
		return writeRootFileAtomic(root, rel, file.Content, renderedMode(file.Mode))
	}
	mode := renderedMode(file.Mode)
	if info.Mode().Perm() != mode {
		if err := root.Chmod(rel, mode); err != nil {
			return fmt.Errorf("repair mode for rendered embedded file %s: %w", file.RelPath, err)
		}
	}
	return nil
}

func ensureRootDirs(root *os.Root, rel string) error {
	if rel == "." || rel == "" {
		return nil
	}
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(rel), "/") {
		if current == "" {
			current = component
		} else {
			current = filepath.ToSlash(filepath.Join(current, component))
		}
		info, err := root.Lstat(filepath.FromSlash(current))
		switch {
		case errorsIsNotExist(err):
			if err := root.Mkdir(filepath.FromSlash(current), 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
			if err := root.RemoveAll(filepath.FromSlash(current)); err != nil {
				return err
			}
			if err := root.Mkdir(filepath.FromSlash(current), 0o755); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeRootFileAtomic(root *os.Root, rel string, content []byte, mode fs.FileMode) (err error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("create temporary name for %s: %w", filepath.ToSlash(rel), err)
	}
	tempRel := filepath.Join(filepath.Dir(rel), "."+filepath.Base(rel)+".brandrender-"+hex.EncodeToString(nonce[:]))
	temp, err := root.OpenFile(tempRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create temporary rendered file for %s: %w", filepath.ToSlash(rel), err)
	}
	tempExists := true
	defer func() {
		if tempExists {
			_ = root.Remove(tempRel)
		}
	}()
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary rendered file for %s: %w", filepath.ToSlash(rel), err)
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set temporary rendered file mode for %s: %w", filepath.ToSlash(rel), err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary rendered file for %s: %w", filepath.ToSlash(rel), err)
	}
	if err := root.Rename(tempRel, rel); err != nil {
		return fmt.Errorf("replace rendered embedded file %s: %w", filepath.ToSlash(rel), err)
	}
	tempExists = false
	return nil
}

func renderedMode(mode fs.FileMode) fs.FileMode {
	mode = mode.Perm()
	if mode == 0 {
		return 0o644
	}
	return mode
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
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
	if err := appendBashPolicyFile(&out, repoRoot, values); err != nil {
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

func appendBashPolicyFile(out *[]RenderedFile, repoRoot string, values brand.Values) error {
	srcPath := filepath.Join(repoRoot, ".bash-policy.yaml")
	file, err := renderSourceFile(srcPath, "bash-policy.yaml", values)
	if err != nil {
		return err
	}
	values = valuesFromDefaults(values)
	file.Content = bytes.ReplaceAll(file.Content, []byte("Bash(liza:*)"), []byte("Bash("+values.BinaryName+":*)"))
	*out = append(*out, file)
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
		{"check-liza-input-readiness", "check-" + values.NameLower + "-input-readiness"},
		{"liza-logs", values.BinaryName + "-logs"},
		{"liza-index", values.BinaryName + "-index"},
		{"liza-session", values.BinaryName + "-session"},
		{"liza-operator", values.NameLower + "-operator"},
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
	// Pass the script via stdin rather than a temp file path. On Windows the
	// temp path contains backslashes (C:\Users\...) which bash interprets as
	// escape characters, mangling it into an unusable path. Reading from stdin
	// sidesteps the path entirely and is portable across platforms.
	cmd := exec.Command(bashPath, "-n")
	cmd.Stdin = bytes.NewReader(content)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("invalid shell syntax: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
