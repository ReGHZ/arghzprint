// Package ui exposes the embedded web UI files.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed index.html assets
var embedded embed.FS

// FS is the sub-filesystem. Paths like "assets/alpine.js" and "assets/ace/ace.js" resolve directly.

// FS is the sub-filesystem rooted at the ui/ directory.
// Handlers serve from this directly so paths like "assets/app.js" resolve correctly.
var FS fs.FS = embedded
