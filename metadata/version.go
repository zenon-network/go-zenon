//go:build !libznn

// Package metadata exposes the build identity of the node binary: the
// release tag compiled in as a constant and the GitCommit recovered
// from the build's embedded VCS information. These values are fixed at
// build time and surface over RPC through stats.processInfo
// (rpc/api/stats.go).
//
// The release workflow extracts the tag by grepping this file for the
// constant's name, so no other line in this file may contain it.
package metadata

const (
	// The release tag compiled into the node binary.
	Version = "v0.0.9"
)
