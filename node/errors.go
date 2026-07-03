package node

import (
	"syscall"

	"github.com/pkg/errors"
)

var (
	// ErrDataDirUsed is returned when the data directory is already
	// locked by another running node instance.
	ErrDataDirUsed = errors.New("dataDir already used by another process")
	// ErrNodeStopped is returned when an operation needs the wallet or
	// Zenon core but the node is not running.
	ErrNodeStopped     = errors.New("node not started")
	datadirInUseErrnos = map[uint]bool{11: true, 32: true, 35: true}
)

func convertFileLockError(err error) error {
	if errno, ok := err.(syscall.Errno); ok && datadirInUseErrnos[uint(errno)] {
		return ErrDataDirUsed
	}
	return err
}
