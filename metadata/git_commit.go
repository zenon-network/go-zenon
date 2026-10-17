package metadata

import "runtime/debug"

// GitCommit is the git revision the binary was built from, read once at
// startup from the vcs.revision setting that the go toolchain embeds in
// the build info. It is empty when the build carries no VCS information,
// for example when built from an unversioned source tree or with VCS
// stamping disabled.
var GitCommit = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}()
