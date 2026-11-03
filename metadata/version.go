//go:build !libznn

// Package metadata exposes the build identity of the node binary: the
// release version and the GitCommit, both injected at build time via
// -ldflags -X. These values surface over RPC through stats.processInfo
// (rpc/api/stats.go).
package metadata

// Version is the release version compiled into the node binary. It
// defaults to "dev" when the build does not inject one.
var Version = "dev"
