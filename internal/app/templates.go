package app

import (
	"fmt"
	"html/template"
	"io"
	"sync"

	"github.com/adammwaniki/jacarandapropaganda/web"
)

// fragmentTemplates parses every html/template file under web/templates/
// exactly once. The cache is process-global because templates are embedded
// into the binary and never change at runtime.
var fragmentTemplates = struct {
	once sync.Once
	tpl  *template.Template
	err  error
}{}

func getFragments() (*template.Template, error) {
	fragmentTemplates.once.Do(func() {
		fragmentTemplates.tpl, fragmentTemplates.err = template.ParseFS(
			web.Templates,
			"templates/fragments/*.html",
		)
	})
	return fragmentTemplates.tpl, fragmentTemplates.err
}

// renderFragment writes the named fragment template to w with data.
// Fragments are always HTML and always small; callers typically set the
// Content-Type header before calling. A template error is fatal for the
// response (the handler should have already written a status code).
func renderFragment(w io.Writer, name string, data any) error {
	tpl, err := getFragments()
	if err != nil {
		return fmt.Errorf("parse fragments: %w", err)
	}
	if err := tpl.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("execute fragment %q: %w", name, err)
	}
	return nil
}
