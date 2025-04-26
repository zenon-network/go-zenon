package cache

import (
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	sporkInfoKeyPrefix = []byte{0}
)

func getSporkInfoKeyPrefix(id []byte) []byte {
	return common.JoinBytes(sporkCacheKeyPrefix, sporkInfoKeyPrefix, id)
}

func (cs *cacheStore) IsSporkActive(implemented *types.ImplementedSpork) (bool, error) {
	identifier := cs.Identifier()
	if identifier.Height == 1 {
		return false, nil
	}

	data, err := cs.findValue(getSporkInfoKeyPrefix(implemented.SporkId.Bytes()))
	if err != nil {
		return false, err
	}

	if len(data) == 0 {
		return false, nil
	}

	spork := definition.ParseSporkInfo(data)
	if spork.Activated && spork.EnforcementHeight <= identifier.Height && spork.Id == implemented.SporkId {
		return true, nil
	}

	return false, nil
}
