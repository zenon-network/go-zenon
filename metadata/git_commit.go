package metadata

import "runtime/debug"

// GitCommit is normally set at build time via -ldflags -X. CommitHash falls
// back to the toolchain-embedded VCS revision for builds that don't inject it.
var GitCommit = ""

func CommitHash() string {
	if GitCommit != "" {
		return GitCommit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}
