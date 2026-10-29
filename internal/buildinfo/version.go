// Package buildinfo identifies the source revision of a running binary.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

// Version is the public release version shared by the panel and MCP server.
const Version = "0.1.0"

// Info contains public build metadata, without local paths or environment values.
type Info struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
	Modified bool   `json:"modified"`
}

// Current reads the VCS metadata embedded by go build. Source archives have no
// Git metadata and explicitly report an unknown revision.
func Current() Info {
	info := Info{Version: Version, Revision: "unknown"}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Revision = setting.Value
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}
	return info
}

// String formats the version for CLI diagnostics.
func String(service string) string {
	info := Current()
	return fmt.Sprintf("%s %s (revision %s, modified=%t)", service, info.Version, info.Revision, info.Modified)
}
