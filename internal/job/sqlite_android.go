//go:build android

package job

// modernc/libc issues a raw lstat that Android's seccomp filter kills (SIGSYS),
// so Android uses the cgo driver — gomobile already builds with the NDK
import _ "github.com/mattn/go-sqlite3"

const sqlDriver = "sqlite3"
