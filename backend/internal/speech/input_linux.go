//go:build linux

package speech

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

const runtimeGOOS = "linux"

func memoryFile(data []byte) (*os.File, error) {
	fd, err := unix.MemfdCreate("speech-audio", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "speech-audio")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("memfd unavailable")
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
