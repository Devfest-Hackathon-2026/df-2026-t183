//go:build !linux

package speech

import "os"

const runtimeGOOS = "other"

func memoryFile([]byte) (*os.File, error) {
	return nil, ErrInternal
}
