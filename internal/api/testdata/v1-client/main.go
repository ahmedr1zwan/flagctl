// This driver exercises the archived client, not the application's current
// client. Keep all imports local to this standalone, standard-library-only module.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: v1-client <loopback-origin>")
		os.Exit(2)
	}
	if err := lifecycle(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("v1 client lifecycle passed")
}

func lifecycle(endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := client.New(endpoint, 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	items, err := c.List(ctx, "dev")
	if err != nil || items == nil || len(items) != 0 {
		return fmt.Errorf("empty list: %v", err)
	}
	created, err := c.Create(ctx, "dev", flags.CreateInput{Key: "checkout"})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if created.Environment != "dev" || created.Key != "checkout" || created.Enabled || created.Description != "" || !created.CreatedAt.Equal(created.UpdatedAt) {
		return errors.New("create changed identity, defaults, or timestamps")
	}
	prod, err := c.Create(ctx, "prod", flags.CreateInput{Key: "checkout", Enabled: true, Description: "production"})
	if err != nil {
		return fmt.Errorf("create in prod: %w", err)
	}
	if _, err := c.Create(ctx, "dev", flags.CreateInput{Key: "checkout", Enabled: true}); !apiError(err, 409, "already_exists") {
		return fmt.Errorf("duplicate create: %v", err)
	}
	read, err := c.Get(ctx, "dev", "checkout")
	if err != nil || read != created {
		return fmt.Errorf("get after duplicate: %v", err)
	}
	for _, key := range []string{"z", "a"} {
		if _, err := c.Create(ctx, "dev", flags.CreateInput{Key: key}); err != nil {
			return err
		}
	}
	items, err = c.List(ctx, "dev")
	if err != nil || len(items) != 3 || items[0].Key != "a" || items[1] != created || items[2].Key != "z" {
		return fmt.Errorf("ordered list: %v", err)
	}
	enabled, description := true, "new checkout"
	updated, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled, Description: &description})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if !updated.Enabled || updated.Description != description || updated.CreatedAt != created.CreatedAt || !updated.UpdatedAt.After(created.UpdatedAt) {
		return errors.New("update changed field or timestamp semantics")
	}
	noOp, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled})
	if err != nil || noOp != updated {
		return fmt.Errorf("no-op update: %v", err)
	}
	enabled = false
	disabled, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled})
	if err != nil || disabled.Enabled || disabled.Description != description || disabled.CreatedAt != created.CreatedAt {
		return fmt.Errorf("explicit false / omitted description: %v", err)
	}
	description = ""
	cleared, err := c.Update(ctx, "dev", "checkout", flags.UpdateInput{Description: &description})
	if err != nil || cleared.Description != "" || cleared.Enabled || cleared.CreatedAt != created.CreatedAt {
		return fmt.Errorf("explicit empty / omitted enabled: %v", err)
	}
	read, err = c.Get(ctx, "prod", "checkout")
	if err != nil || read != prod {
		return fmt.Errorf("environment isolation: %v", err)
	}
	for _, key := range []string{"a", "checkout", "z"} {
		if err := c.Delete(ctx, "dev", key); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
	}
	if err := c.Delete(ctx, "prod", "checkout"); err != nil {
		return err
	}
	_, err = c.Get(ctx, "dev", "checkout")
	if !apiError(err, 404, "not_found") {
		return fmt.Errorf("missing read: %v", err)
	}
	_, err = c.Update(ctx, "dev", "checkout", flags.UpdateInput{Enabled: &enabled})
	if !apiError(err, 404, "not_found") {
		return fmt.Errorf("missing update: %v", err)
	}
	if err := c.Delete(ctx, "dev", "checkout"); !apiError(err, 404, "not_found") {
		return fmt.Errorf("missing delete: %v", err)
	}
	items, err = c.List(ctx, "dev")
	if err != nil || items == nil || len(items) != 0 {
		return fmt.Errorf("list after deletion: %v", err)
	}
	return nil
}

func apiError(err error, status int, code string) bool {
	var response *client.APIError
	return errors.As(err, &response) && response.StatusCode == status && response.Code == code
}
