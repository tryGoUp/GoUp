package assets

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*
var templatesFS embed.FS

var (
	ErrorTemplate   *template.Template
	WelcomeTemplate *template.Template
	GlobalStyles    template.HTML
)

func init() {
	var err error

	// Read embedded CSS
	cssContent, err := templatesFS.ReadFile("templates/style.css")
	if err != nil {
		panic(err)
	}
	// Wrap in style tags to avoid formatter issues in HTML templates
	GlobalStyles = template.HTML("<style>" + string(cssContent) + "</style>")

	ErrorTemplate, err = template.ParseFS(templatesFS, "templates/error.html")
	if err != nil {
		panic(err)
	}
	WelcomeTemplate, err = template.ParseFS(templatesFS, "templates/welcome.html")
	if err != nil {
		panic(err)
	}
}

// ErrorPageData holds data for the error page template
type ErrorPageData struct {
	Code        int
	Title       string
	Description string
	FooterLink  string
	Styles      template.HTML
}

// RenderErrorPage renders the error page to the response writer
func RenderErrorPage(w http.ResponseWriter, code int, title, description string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)

	data := ErrorPageData{
		Code:        code,
		Title:       title,
		Description: description,
		FooterLink:  "https://github.com/tryGoUp",
		Styles:      GlobalStyles,
	}

	if err := ErrorTemplate.Execute(w, data); err != nil {
		// Fallback if template fails
		http.Error(w, description, code)
	}
}

// RenderWelcomePage renders the welcome page
func RenderWelcomePage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	data := struct {
		FooterLink string
		Styles     template.HTML
	}{
		FooterLink: "https://github.com/tryGoUp",
		Styles:     GlobalStyles,
	}

	if err := WelcomeTemplate.Execute(w, data); err != nil {
		http.Error(w, "Welcome to GoUp!", http.StatusOK)
	}
}
