//go:build libznn && !detached

package main

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// runNodeWithConfigString is a test helper that wraps
// RunNodeWithConfig, converting a Go string to a C string so the guard
// clauses can be exercised from Go test code (cgo is not supported in
// _test.go files). A nil input passes a nil *C.char to test the nil
// guard.
func runNodeWithConfigString(s *string) int {
	if s == nil {
		return int(RunNodeWithConfig(nil))
	}
	cs := C.CString(*s)
	defer C.free(unsafe.Pointer(cs))
	return int(RunNodeWithConfig(cs))
}
