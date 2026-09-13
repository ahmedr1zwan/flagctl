package store

import (
	"errors"
	"os"
	"path/filepath"
)

func prepareDatabase(directory string) (string, error) {
	if directory == "" {
		return "", errors.New("--data-dir must not be empty")
	}
	path, err := filepath.Abs(directory)
	if err != nil {
		return "", errors.New("could not resolve --data-dir")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", errors.New("could not create data directory; check parent directory permissions")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("data directory must be a real directory, not a symlink")
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", errors.New("data directory must be private (chmod 700); use a dedicated --data-dir")
	}
	database := filepath.Join(path, "flags.db")
	file, err := os.OpenFile(database, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		if err := file.Close(); err != nil {
			return "", errors.New("could not close newly created database file")
		}
	} else if !errors.Is(err, os.ErrExist) {
		return "", errors.New("could not create database file; check data directory permissions")
	}
	// Reject unsafe existing files instead of following links or silently changing
	// user permissions. The private directory also protects SQLite sidecar files.
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		info, err := os.Lstat(database + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("database and sidecars must be regular files, not symlinks")
		}
		if info.Mode().Perm()&0077 != 0 {
			return "", errors.New("database and sidecars must be private (chmod 600)")
		}
	}
	return database, nil
}
