// Package buildinfo exposes release identity without accessing Git at runtime.
package buildinfo

import "fmt"

// Revision and BuildTime are injected by the release builder; local builds
// explicitly identify themselves as development builds rather than releases.
var Revision = "development"
var BuildTime = "unknown"

// Description returns a compact server identity for CLI/support diagnostics.
func Description() string { return fmt.Sprintf("omniHub %s (%s)", Revision, BuildTime) }
