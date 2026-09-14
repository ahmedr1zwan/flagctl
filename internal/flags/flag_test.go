package flags_test

import (
	"strings"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

func TestIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{"single letter", "a", true},
		{"single digit", "0", true},
		{"separators", "checkout_v2-prod", true},
		{"maximum length", strings.Repeat("a", 63), true},
		{"too long", strings.Repeat("a", 64), false},
		{"empty", "", false},
		{"uppercase", "DEV", false},
		{"leading separator", "_dev", false},
		{"space", "dev prod", false},
		{"path", "../dev", false},
		{"non ASCII", "développement", false},
		{"newline", "dev\n", false},
		{"invalid UTF8", string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, validate := range map[string]func(string) error{
				"environment": flags.ValidateEnvironment, "key": flags.ValidateKey,
			} {
				if err := validate(tc.value); (err == nil) != tc.valid {
					t.Errorf("%s validation: got %v, want valid=%t", name, err, tc.valid)
				}
			}
		})
	}
}

func TestDescriptionByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{"empty", "", true},
		{"ASCII boundary", strings.Repeat("a", 1024), true},
		{"ASCII too long", strings.Repeat("a", 1025), false},
		{"UTF8 boundary", strings.Repeat("é", 512), true},
		{"UTF8 too long", strings.Repeat("é", 513), false},
		{"invalid UTF8", string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := flags.ValidateDescription(tc.value); (err == nil) != tc.valid {
				t.Errorf("got %v, want valid=%t", err, tc.valid)
			}
		})
	}
}

func TestInputsValidateIdentityAndPartialValues(t *testing.T) {
	disabled, empty, tooLong := false, "", strings.Repeat("x", 1025)
	for _, tc := range []struct {
		name  string
		input flags.UpdateInput
		valid bool
	}{
		{"no fields", flags.UpdateInput{}, false},
		{"explicit false", flags.UpdateInput{Enabled: &disabled}, true},
		{"clear description", flags.UpdateInput{Description: &empty}, true},
		{"both fields", flags.UpdateInput{Enabled: &disabled, Description: &empty}, true},
		{"bad description", flags.UpdateInput{Enabled: &disabled, Description: &tooLong}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.input.Validate("dev", "checkout"); (err == nil) != tc.valid {
				t.Errorf("got %v, want valid=%t", err, tc.valid)
			}
		})
	}
	for _, identity := range [][2]string{{"dev", "checkout"}, {"DEV", "checkout"}, {"dev", "../checkout"}} {
		valid := identity == [2]string{"dev", "checkout"}
		create := flags.CreateInput{Key: identity[1]}
		update := flags.UpdateInput{Enabled: &disabled}
		if err := create.Validate(identity[0]); (err == nil) != valid {
			t.Errorf("create identity validation: %v", err)
		}
		if err := update.Validate(identity[0], identity[1]); (err == nil) != valid {
			t.Errorf("update identity validation: %v", err)
		}
	}
	if err := (flags.CreateInput{Key: "checkout", Description: tooLong}).Validate("dev"); err == nil {
		t.Fatal("create accepted an oversized description")
	}
}
