package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

//go:embed templates/blocks/*.tmpl
var blocksFS embed.FS

var funcMap = template.FuncMap{
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"sub": func(a, b int) int {
		return a - b
	},
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
