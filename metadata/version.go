//go:build !libznn

// Package metadata exposes the build identity of the node binary: the
// release Version compiled in as a constant and the GitCommit recovered
// from the build's embedded VCS information. These values are fixed at
// build time and surface over RPC through stats.processInfo
// (rpc/api/stats.go).
package metadata

const (
	// Version is the release version compiled into the binary, for
	// example "v0.0.8". Shared-library (libznn) builds override this
	// with a "-libznn"-suffixed value in version_libznn.go.
	Version = "v0.0.8"
)
