// Package store persists flags in a local SQLite database.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	_ "modernc.org/sqlite"
)

var (
	ErrAlreadyExists = errors.New("flag already exists")
	ErrNotFound      = errors.New("flag not found")
)

type Store struct {
	db *sql.DB
}

// Open creates or opens flags.db within a private, application-owned directory.
// The caller owns Close and must use a trusted local filesystem directory.
func Open(ctx context.Context, directory string) (*Store, error) {
	path, err := prepareDatabase(directory)
	if err != nil {
		return nil, err
	}
	// Construct the URI ourselves: a directory name cannot inject SQLite options.
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{
		"mode":    {"rw"},
		"_pragma": {"busy_timeout(5000)", "journal_mode(DELETE)", "synchronous(FULL)"},
	}
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, errors.New("could not open SQLite database")
	}
	// A single connection serializes this local service's writes and avoids
	// competing SQLite writers within the process. Busy waits are bounded.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initialize(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("could not initialize SQLite; check file access and database locks")
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return errors.New("could not read SQLite schema version")
	}
	switch version {
	case 0:
		_, err := tx.ExecContext(ctx, `CREATE TABLE flags (
			environment TEXT NOT NULL,
			key TEXT NOT NULL,
			description TEXT NOT NULL,
			enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (environment, key)
		) STRICT`)
		if err != nil {
			return errors.New("could not create flag schema; use a dedicated flagctl data directory")
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return errors.New("could not record SQLite schema version")
		}
	case 1:
		// No migration is needed for the current schema.
	default:
		return errors.New("unsupported database schema version; use a compatible flagd binary")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("could not commit SQLite schema initialization")
	}
	return nil
}

func (s *Store) Create(ctx context.Context, environment string, input flags.CreateInput) (flags.Flag, error) {
	if err := input.Validate(environment); err != nil {
		return flags.Flag{}, err
	}
	now := time.Now().UTC()
	flag := flags.Flag{
		Environment: environment, Key: input.Key, Description: input.Description,
		Enabled: input.Enabled, CreatedAt: now, UpdatedAt: now,
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO flags
		(environment, key, description, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (environment, key) DO NOTHING`,
		flag.Environment, flag.Key, flag.Description, flag.Enabled,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return flags.Flag{}, fmt.Errorf("create flag: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return flags.Flag{}, fmt.Errorf("create flag result: %w", err)
	}
	if count == 0 {
		return flags.Flag{}, ErrAlreadyExists
	}
	return flag, nil
}

func (s *Store) Get(ctx context.Context, environment, key string) (flags.Flag, error) {
	if err := flags.ValidateIdentity(environment, key); err != nil {
		return flags.Flag{}, err
	}
	flag, err := scanFlag(s.db.QueryRowContext(ctx, `SELECT
		key, environment, description, enabled, created_at, updated_at
		FROM flags WHERE environment = ? AND key = ?`, environment, key))
	if errors.Is(err, sql.ErrNoRows) {
		return flags.Flag{}, ErrNotFound
	}
	return flag, err
}

// Update reads and writes within one transaction. Concurrent partial updates
// preserve omitted fields, and a no-op keeps the original update timestamp.
func (s *Store) Update(ctx context.Context, environment, key string, input flags.UpdateInput) (flags.Flag, error) {
	if err := input.Validate(environment, key); err != nil {
		return flags.Flag{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return flags.Flag{}, err
	}
	defer tx.Rollback()
	current, err := scanFlag(tx.QueryRowContext(ctx, `SELECT
		key, environment, description, enabled, created_at, updated_at
		FROM flags WHERE environment = ? AND key = ?`, environment, key))
	if errors.Is(err, sql.ErrNoRows) {
		return flags.Flag{}, ErrNotFound
	}
	if err != nil {
		return flags.Flag{}, err
	}
	updated := current
	if input.Enabled != nil {
		updated.Enabled = *input.Enabled
	}
	if input.Description != nil {
		updated.Description = *input.Description
	}
	if updated.Enabled != current.Enabled || updated.Description != current.Description {
		updated.UpdatedAt = time.Now().UTC()
		_, err := tx.ExecContext(ctx, `UPDATE flags SET enabled = ?, description = ?, updated_at = ?
			WHERE environment = ? AND key = ?`, updated.Enabled, updated.Description,
			updated.UpdatedAt.Format(time.RFC3339Nano), environment, key)
		if err != nil {
			return flags.Flag{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return flags.Flag{}, err
	}
	return updated, nil
}

func (s *Store) Delete(ctx context.Context, environment, key string) error {
	if err := flags.ValidateIdentity(environment, key); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM flags WHERE environment = ? AND key = ?", environment, key)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) List(ctx context.Context, environment string) ([]flags.Flag, error) {
	if err := flags.ValidateEnvironment(environment); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT
		key, environment, description, enabled, created_at, updated_at
		FROM flags WHERE environment = ? ORDER BY key ASC`, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]flags.Flag, 0)
	for rows.Next() {
		flag, err := scanFlag(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, flag)
	}
	return result, rows.Err()
}

func scanFlag(row interface{ Scan(...any) error }) (flags.Flag, error) {
	var flag flags.Flag
	var created, updated string
	if err := row.Scan(&flag.Key, &flag.Environment, &flag.Description, &flag.Enabled, &created, &updated); err != nil {
		return flags.Flag{}, err
	}
	var err error
	if flag.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return flags.Flag{}, errors.New("invalid stored creation timestamp")
	}
	if flag.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return flags.Flag{}, errors.New("invalid stored update timestamp")
	}
	return flag, nil
}
