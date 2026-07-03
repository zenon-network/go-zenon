//go:build libznn

package metadata

const (
	// Version is the release version compiled into the shared library
	// (libznn) build; it carries a "-libznn" suffix to distinguish it
	// from the standalone node binary's version.
	Version = "v0.0.8-libznn"
)
