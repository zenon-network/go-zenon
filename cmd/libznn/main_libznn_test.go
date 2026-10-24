//go:build libznn && !detached

package main

import "testing"

// RunNodeWithConfig must return 1 (not panic or exit) when passed a nil
// config pointer.
func TestRunNodeWithConfig_NilReturnsError(t *testing.T) {
	if got := runNodeWithConfigString(nil); got != 1 {
		t.Errorf("RunNodeWithConfig(nil) = %d, want 1", got)
	}
}

// RunNodeWithConfig must return 1 when passed an empty string.
func TestRunNodeWithConfig_EmptyReturnsError(t *testing.T) {
	empty := ""
	if got := runNodeWithConfigString(&empty); got != 1 {
		t.Errorf("RunNodeWithConfig(\"\") = %d, want 1", got)
	}
}

// RunNodeWithConfig must return 1 when passed malformed JSON.
func TestRunNodeWithConfig_MalformedJSONReturnsError(t *testing.T) {
	bad := "{not-valid-json"
	if got := runNodeWithConfigString(&bad); got != 1 {
		t.Errorf("RunNodeWithConfig(malformed) = %d, want 1", got)
	}
}
