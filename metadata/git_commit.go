package metadata

import "runtime/debug"

// GitCommit is the git revision the binary was built from, injected at
// build time via -ldflags -X. It is empty when the build does not set it.
var GitCommit = ""

// CommitHash returns GitCommit, falling back to the vcs.revision setting
// that the go toolchain embeds in the build info. It returns "" when
// neither is available.
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
