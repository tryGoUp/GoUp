package assets

import (
	"html/template"
	"net/http"
)

var (
	ErrorTemplate   *template.Template
	WelcomeTemplate *template.Template
	ListingTemplate *template.Template
	GlobalStyles    template.HTML
)

func init() {
	var err error

	// Initialize global styles
	GlobalStyles = template.HTML("<style>" + StyleCSS + "</style>")

	ErrorTemplate, err = template.New("error").Parse(ErrorHTML)
	if err != nil {
		panic(err)
	}
	WelcomeTemplate, err = template.New("welcome").Parse(WelcomeHTML)
	if err != nil {
		panic(err)
	}
	ListingTemplate, err = template.New("listing").Parse(ListingHTML)
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

// ListingPageData holds data for the directory listing template
type ListingPageData struct {
	Path       string
	Items      []ListingItem
	ShowBack   bool
	FooterLink string
	Styles     template.HTML
}

// ListingItem represents a file or directory in the listing
type ListingItem struct {
	Name    string
	IsDir   bool
	Size    string
	ModTime string
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

// RenderDirectoryListing renders the directory listing page
func RenderDirectoryListing(w http.ResponseWriter, path string, items []ListingItem, showBack bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	data := ListingPageData{
		Path:       path,
		Items:      items,
		ShowBack:   showBack,
		FooterLink: "https://github.com/tryGoUp",
		Styles:     GlobalStyles,
	}

	if err := ListingTemplate.Execute(w, data); err != nil {
		http.Error(w, "Directory Listing", http.StatusOK)
	}
}
