// Package mcpwhatsapp holds the version, the single source of truth for
// releases: VERSION = the section in CHANGELOG.md = the tag vX.Y.Z.
package mcpwhatsapp

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version of this build.
func Version() string { return strings.TrimSpace(version) }
