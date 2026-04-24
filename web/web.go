// Package web embeds HTML templates and static assets. Kept as a separate
// package so the compiled binary carries everything it needs to serve HTTP.
package web

import "embed"

//go:embed templates/*.html
var Templates embed.FS
