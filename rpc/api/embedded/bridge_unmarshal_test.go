package embedded

import (
	"encoding/json"
	"testing"
)

// TestWrapTokenRequestUnmarshalEmpty verifies that decoding an empty or
// null JSON object into WrapTokenRequest does not panic. The embedded
// WrapTokenRequestMarshal pointer must be initialised before
// json.Unmarshal so that promoted field access is safe even when the
// input carries none of the Marshal struct's fields.
func TestWrapTokenRequestUnmarshalEmpty(t *testing.T) {
	for _, input := range []string{`{}`, `null`} {
		var w WrapTokenRequest
		if err := json.Unmarshal([]byte(input), &w); err != nil {
			t.Fatalf("input %s: unexpected error: %v", input, err)
		}
		if w.WrapTokenRequest == nil {
			t.Fatalf("input %s: WrapTokenRequest is nil after decode", input)
		}
	}
}

// TestUnwrapTokenRequestUnmarshalEmpty verifies the same nil-safety for
// UnwrapTokenRequest.
func TestUnwrapTokenRequestUnmarshalEmpty(t *testing.T) {
	for _, input := range []string{`{}`, `null`} {
		var u UnwrapTokenRequest
		if err := json.Unmarshal([]byte(input), &u); err != nil {
			t.Fatalf("input %s: unexpected error: %v", input, err)
		}
		if u.UnwrapTokenRequest == nil {
			t.Fatalf("input %s: UnwrapTokenRequest is nil after decode", input)
		}
	}
}
