package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

//go:embed templates/blocks/*.tmpl
var blocksFS embed.FS

var funcMap = template.FuncMap{
	"binaryName":               promptBinaryName,
	"brandTitle":               promptBrandTitle,
	"destructiveDBAllowMarker": models.CurrentDestructiveDBAllowMarker,
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"globalDirName":  promptGlobalDirName,
	"projectDirName": promptProjectDirName,
	"sub": func(a, b int) int {
		return a - b
	},
}

func promptBinaryName() string {
	return brand.RuntimeValues().BinaryName
}

func promptBrandTitle() string {
	return brand.RuntimeValues().NameTitle
}

func promptGlobalDirName() string {
	return brand.RuntimeValues().GlobalDirName
}

func promptProjectDirName() string {
	return brand.RuntimeValues().ProjectDirName
}

var tmpl = template.Must(
	template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.tmpl"),
)

var blockTmpl = template.Must(
	template.New("").Funcs(funcMap).ParseFS(blocksFS, "templates/blocks/*.tmpl"),
)

func executeTemplate(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	return buf.String(), nil
}
