//go:build unix

package security

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOpenRejectsReplacementSymlinkAndDoesNotBlockOnFIFO(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if file, err := openFile(link); err == nil {
		file.Close()
		t.Fatal("open followed symlink")
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openFile(fifo)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := readFile(fifo, true, 128); err == nil {
		t.Fatal("FIFO accepted as credential file")
	}
}
