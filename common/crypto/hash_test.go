package crypto

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func TestEmptyHash(t *testing.T) {
	h := Hash()
	common.ExpectBytes(t, h, `0xa7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a`)
}
