// Package defaulttemplates provides the built-in receipt templates.
// Copied to the user's template directory on first run if not already present.
package defaulttemplates

import "embed"

//go:embed kitchen.html bar.html customer.html billiard.html
var FS embed.FS
