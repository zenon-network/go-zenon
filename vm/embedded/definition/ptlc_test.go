package definition

import (
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
)

func TestPtlcInfoKeyParsing(t *testing.T) {
	id := types.Hash{}
	validKey := getPtlcInfoKey(id)

	tests := []struct {
		name    string
		key     []byte
		wantErr bool
	}{
		{name: "empty", key: nil, wantErr: true},
		{name: "short", key: validKey[:len(validKey)-1], wantErr: true},
		{name: "long", key: append(append([]byte{}, validKey...), 0), wantErr: true},
		{name: "wrong prefix", key: append([]byte{ptlcInfoKeyPrefix[0] + 1}, id.Bytes()...), wantErr: true},
		{name: "valid", key: validKey, wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("unmarshalPtlcInfoKey panicked: %v", recovered)
				}
			}()

			got, err := unmarshalPtlcInfoKey(test.key)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *got != id {
				t.Fatalf("unexpected id: got %v want %v", got, id)
			}
		})
	}
}
