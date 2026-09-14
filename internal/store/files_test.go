package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

func TestPrivateDatabaseAndURIPath(t *testing.T) {
	t.Parallel()
	// URI metacharacters must be literal path characters, not SQLite options.
	directory := filepath.Join(t.TempDir(), "data ?mode=memory#test")
	s := openStore(t, directory)
	created, err := s.Create(t.Context(), "dev", flags.CreateInput{Key: "persisted"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{directory: 0700, filepath.Join(directory, "flags.db"): 0600} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Errorf("%s mode = %o, want %o", filepath.Base(path), info.Mode().Perm(), mode)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, directory)
	if got, err := s.Get(t.Context(), "dev", "persisted"); err != nil || got != created {
		t.Fatalf("URI-like path did not persist: %+v, %v", got, err)
	}
}

func TestRejectUnsafeFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission and symlink protections target Unix filesystems")
	}
	t.Parallel()
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		for _, kind := range []string{"symlink", "public", "directory"} {
			t.Run("flags.db"+suffix+"/"+kind, func(t *testing.T) {
				directory := filepath.Join(t.TempDir(), "data")
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(directory, "flags.db"+suffix)
				target := filepath.Join(t.TempDir(), "untouched")
				marker := []byte("synthetic-sensitive-file")
				if err := os.WriteFile(target, marker, 0600); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "symlink":
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				case "public":
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0640); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if s, err := store.Open(t.Context(), directory); err == nil {
					s.Close()
					t.Fatal("accepted unsafe database/sidecar")
				}
				if got, err := os.ReadFile(target); err != nil || string(got) != string(marker) {
					t.Fatalf("changed symlink target: %q, %v", got, err)
				}
			})
		}
	}
	for _, kind := range []string{"public directory", "directory symlink"} {
		t.Run(kind, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "data")
			if kind == "public directory" {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(directory, 0750); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(t.TempDir(), directory); err != nil {
				t.Fatal(err)
			}
			if s, err := store.Open(t.Context(), directory); err == nil {
				s.Close()
				t.Fatal("accepted unsafe data directory")
			}
			if _, err := os.Lstat(filepath.Join(directory, "flags.db")); !os.IsNotExist(err) {
				t.Fatalf("created database in rejected directory: %v", err)
			}
		})
	}
}

func TestRejectNewerSchemaWithoutChangingData(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "data")
	s := openStore(t, directory)
	if _, err := s.Create(t.Context(), "dev", flags.CreateInput{Key: "untouched"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, "flags.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if reopened, err := store.Open(t.Context(), directory); err == nil {
		reopened.Close()
		t.Fatal("accepted an unsupported schema version")
	} else if !strings.Contains(err.Error(), "unsupported database schema version") {
		t.Fatal(err)
	}
	var version, count int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM flags WHERE key = 'untouched'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if version != 2 || count != 1 {
		t.Fatalf("rejected schema was changed: version=%d, records=%d", version, count)
	}
}
