//go:build tools

package mobile

// gomobile bind needs x/mobile/bind in go.mod, but nothing imports it until bind
// generates code — this keeps go mod tidy from dropping it
import _ "golang.org/x/mobile/bind"
