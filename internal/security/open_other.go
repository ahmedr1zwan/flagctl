//go:build !unix

package security

import "os"

const privateFilesSupported = false

// Secret-file loading is disabled on platforms without POSIX permissions.
func openFile(path string) (*os.File, error) { return os.Open(path) }
