package crypto

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func TestEmptyHash(t *testing.T) {
	h := Hash()
	common.ExpectBytes(t, h, `0xa7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a`)
}

func TestEmptyHashSHA256(t *testing.T) {
	h := HashSHA256()
	common.ExpectBytes(t, h, `0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`)
}
