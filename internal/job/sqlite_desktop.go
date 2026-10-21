//go:build !android

package job

// pure Go so the Windows binary still cross-compiles from Linux without cgo
import _ "modernc.org/sqlite"

const sqlDriver = "sqlite"
