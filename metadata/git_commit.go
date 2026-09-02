package metadata

// GitCommit is the git revision the binary was built from, injected at
// build time via -ldflags -X. It is empty when the build does not set it.
var GitCommit = ""
