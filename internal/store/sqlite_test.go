package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

func openStore(t *testing.T, directory string) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestLifecycleAndPersistence(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "data")
	s := openStore(t, directory)
	ctx := t.Context()
	items, err := s.List(ctx, "dev")
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty list = %#v, %v", items, err)
	}
	// SQL-like text must remain data, including after an update and reopen.
	description := "New checkout'); DROP TABLE flags; --"
	created, err := s.Create(ctx, "dev", flags.CreateInput{Key: "checkout", Description: description})
	if err != nil {
		t.Fatal(err)
	}
	if created.Enabled || created.Description != description || created.CreatedAt.IsZero() || !created.CreatedAt.Equal(created.UpdatedAt) || created.CreatedAt.Location() != time.UTC {
		t.Fatalf("invalid creation defaults/timestamps: %+v", created)
	}
	prod, err := s.Create(ctx, "prod", flags.CreateInput{Key: "checkout", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "dev", flags.CreateInput{Key: "checkout", Enabled: true}); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("duplicate create error = %v", err)
	}
	if got, err := s.Get(ctx, "dev", "checkout"); err != nil || got != created {
		t.Fatalf("duplicate changed the original: %+v, %v", got, err)
	}
	enabled := true
	updated, err := s.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled})
	if err != nil || !updated.Enabled || updated.Description != description || !updated.CreatedAt.Equal(created.CreatedAt) || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("partial update = %+v, %v", updated, err)
	}
	if got, err := s.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled}); err != nil || got != updated {
		t.Fatalf("no-op update changed record/timestamps: %+v, %v", got, err)
	}
	empty := ""
	cleared, err := s.Update(ctx, "dev", "checkout", flags.UpdateInput{Description: &empty})
	if err != nil || !cleared.Enabled || cleared.Description != "" {
		t.Fatalf("description-only update = %+v, %v", cleared, err)
	}
	enabled = false
	updated, err = s.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled, Description: &description})
	if err != nil || updated.Enabled || updated.Description != description {
		t.Fatalf("explicit false/combined update = %+v, %v", updated, err)
	}
	for _, key := range []string{"z", "a"} {
		if _, err := s.Create(ctx, "dev", flags.CreateInput{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	items, err = s.List(ctx, "dev")
	if err != nil || len(items) != 3 {
		t.Fatalf("list = %+v, %v", items, err)
	}
	if got := []string{items[0].Key, items[1].Key, items[2].Key}; !reflect.DeepEqual(got, []string{"a", "checkout", "z"}) {
		t.Fatalf("keys not sorted: %v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, directory)
	if got, err := s.Get(ctx, "dev", "checkout"); err != nil || got != updated {
		t.Fatalf("update did not survive reopen: %+v, %v", got, err)
	}
	if err := s.Delete(ctx, "dev", "checkout"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, directory)
	if _, err := s.Get(ctx, "dev", "checkout"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted flag survived reopen: %v", err)
	}
	if _, err := s.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update of missing flag = %v", err)
	}
	if err := s.Delete(ctx, "dev", "checkout"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("repeated delete = %v", err)
	}
	if got, err := s.Get(ctx, "prod", "checkout"); err != nil || got != prod {
		t.Fatalf("dev operations changed prod: %+v, %v", got, err)
	}
}

func TestInvalidAndCanceledOperationsDoNotMutate(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := t.Context()
	created, err := s.Create(ctx, "dev", flags.CreateInput{Key: "checkout"})
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	for _, identity := range [][2]string{{"../dev", "checkout"}, {"dev", "../checkout"}} {
		if _, err := s.Create(ctx, identity[0], flags.CreateInput{Key: identity[1]}); err == nil {
			t.Error("create accepted invalid identity")
		}
		if _, err := s.Get(ctx, identity[0], identity[1]); err == nil {
			t.Error("get accepted invalid identity")
		}
		if _, err := s.Update(ctx, identity[0], identity[1], flags.UpdateInput{Enabled: &enabled}); err == nil {
			t.Error("update accepted invalid identity")
		}
		if err := s.Delete(ctx, identity[0], identity[1]); err == nil {
			t.Error("delete accepted invalid identity")
		}
	}
	if _, err := s.List(ctx, "../dev"); err == nil {
		t.Error("list accepted invalid environment")
	}
	if _, err := s.Update(ctx, "dev", "checkout", flags.UpdateInput{}); err == nil {
		t.Error("empty update succeeded")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for name, operation := range map[string]func() error{
		"create": func() error { _, err := s.Create(canceled, "dev", flags.CreateInput{Key: "new"}); return err },
		"get":    func() error { _, err := s.Get(canceled, "dev", "checkout"); return err },
		"list":   func() error { _, err := s.List(canceled, "dev"); return err },
		"update": func() error {
			_, err := s.Update(canceled, "dev", "checkout", flags.UpdateInput{Enabled: &enabled})
			return err
		},
		"delete": func() error { return s.Delete(canceled, "dev", "checkout") },
	} {
		if err := operation(); !errors.Is(err, context.Canceled) {
			t.Errorf("canceled %s = %v", name, err)
		}
	}
	if got, err := s.List(ctx, "dev"); err != nil || !reflect.DeepEqual(got, []flags.Flag{created}) {
		t.Fatalf("invalid/canceled operations changed data: %+v, %v", got, err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	errorsByOperation := together(8, func(_ int) error {
		_, err := s.Create(ctx, "dev", flags.CreateInput{Key: "checkout"})
		return err
	})
	created, duplicates := 0, 0
	for _, err := range errorsByOperation {
		switch {
		case err == nil:
			created++
		case errors.Is(err, store.ErrAlreadyExists):
			duplicates++
		default:
			t.Fatal(err)
		}
	}
	if created != 1 || duplicates != 7 {
		t.Fatalf("concurrent creates: %d created, %d duplicates", created, duplicates)
	}
	for round := range 12 {
		enabled := round%2 == 0
		description := fmt.Sprintf("round %d", round)
		for _, err := range together(2, func(n int) error {
			input := flags.UpdateInput{Enabled: &enabled}
			if n == 1 {
				input = flags.UpdateInput{Description: &description}
			}
			_, err := s.Update(ctx, "dev", "checkout", input)
			return err
		}) {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got, err := s.Get(ctx, "dev", "checkout"); err != nil || got.Enabled != enabled || got.Description != description {
			t.Fatalf("concurrent partial updates lost a field: %+v, %v", got, err)
		}
	}
	for round := range 12 {
		key := fmt.Sprintf("delete_race_%d", round)
		if _, err := s.Create(ctx, "dev", flags.CreateInput{Key: key}); err != nil {
			t.Fatal(err)
		}
		enabled := true
		for n, err := range together(2, func(n int) error {
			if n == 0 {
				return s.Delete(ctx, "dev", key)
			}
			_, err := s.Update(ctx, "dev", key, flags.UpdateInput{Enabled: &enabled})
			return err
		}) {
			if err != nil && !(n == 1 && errors.Is(err, store.ErrNotFound)) {
				t.Fatalf("delete/update race: %v", err)
			}
		}
		if _, err := s.Get(ctx, "dev", key); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("update recreated deleted flag: %v", err)
		}
	}
}

// Start concurrent operations together and return their errors in input order.
// Assertions stay on the test goroutine, after all workers have finished.
func together(count int, operation func(int) error) []error {
	start, done := make(chan struct{}), make(chan int, count)
	results := make([]error, count)
	for n := range count {
		go func() {
			<-start
			results[n] = operation(n)
			done <- n
		}()
	}
	close(start)
	for range count {
		<-done
	}
	return results
}
