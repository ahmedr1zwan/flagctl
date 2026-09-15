//go:build unix

package security

import (
	"os"
	"syscall"
)

const privateFilesSupported = true

func openFile(path string) (*os.File, error) {
	// Refuse a symlink swapped in after Lstat. A replacement FIFO must not block
	// startup; the caller rejects anything but the same regular file after open.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
