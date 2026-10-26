// Package version reports which bud build is running
package version

import (
	"runtime/debug"
	"strings"
)

// Version is set at release time with -ldflags "-X jgarner.dev/bud/internal/version.Version=..."
var Version = ""

// String returns the release version, or else the module version Go stamps into
// the build (a pseudo-version like 0.1.1-0.20261026053815-cd37a8e46cdd+dirty for
// a local checkout), or "dev" when there's no version info at all
func String() string {
	if Version != "" {
		return strings.TrimPrefix(Version, "v")
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	return fromBuildInfo(info)
}

func fromBuildInfo(info *debug.BuildInfo) string {
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "dev"
	}

	return strings.TrimPrefix(v, "v")
}
