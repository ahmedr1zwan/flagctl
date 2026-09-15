package security

import (
	"errors"
	"io"
	"os"
)

func readFile(path string, private bool, limit int64) ([]byte, error) {
	invalid := errors.New("invalid credential or certificate file")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, invalid
	}
	// POSIX mode bits cannot establish Windows ACL privacy. Fail closed for
	// secrets there until ACL-aware handling is implemented.
	if private && (!privateFilesSupported || info.Mode().Perm()&0077 != 0) {
		return nil, invalid
	}
	file, err := openFile(path)
	if err != nil {
		return nil, invalid
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() ||
		(private && opened.Mode().Perm()&0077 != 0) {
		return nil, invalid
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, invalid
	}
	return data, nil
}
