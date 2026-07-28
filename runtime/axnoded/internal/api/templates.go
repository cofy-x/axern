package api

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html.tmpl
var templateFS embed.FS

var (
	rootPageTemplate      = mustParseTemplate("templates/root.html.tmpl")
	dashboardPageTemplate = mustParseTemplate("templates/dashboard.html.tmpl")
)

func mustParseTemplate(path string) *template.Template {
	return template.Must(template.ParseFS(templateFS, path))
}
