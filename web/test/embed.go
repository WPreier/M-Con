// Package testui embeds the admin test page.
package testui

import "embed"

// Files is the test page served at /test/.
//
//go:embed index.html app.js style.css
var Files embed.FS
