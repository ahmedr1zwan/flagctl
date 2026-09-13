package cli

import (
	"errors"
	"fmt"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/spf13/cobra"
)

func newCreateCommand(options *options) *cobra.Command {
	var input flags.CreateInput
	command := &cobra.Command{
		Use:     "create KEY",
		Short:   "Create a flag, disabled by default",
		Example: "  flagctl flags create checkout_v2 --env dev --description \"New checkout\"\n  flagctl flags create checkout_v2 --env prod --enabled=true --output json",
		Args:    oneKeyArgument,
		RunE: func(command *cobra.Command, args []string) error {
			input.Key = args[0]
			if err := input.Validate(options.environment); err != nil {
				return err
			}
			service, err := options.client(command)
			if err != nil {
				return err
			}
			defer service.Close()
			created, err := service.Create(command.Context(), options.environment, input)
			if err != nil {
				return err
			}
			if err := writeOutput(command.OutOrStdout(), options.output, []flags.Flag{created}, created); err != nil {
				return errors.New("flag was created, but output could not be written; use flags list to confirm")
			}
			return nil
		},
	}
	command.Flags().StringVar(&input.Description, "description", "", "description (at most 1024 UTF-8 bytes)")
	command.Flags().BoolVar(&input.Enabled, "enabled", false, "initial enabled state; use --enabled=true or --enabled=false")
	return command
}

func newListCommand(options *options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List flags in an environment",
		Example: "  flagctl flags list --env dev\n  flagctl flags list --env prod --output json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("flags list does not accept positional arguments")
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := options.client(command)
			if err != nil {
				return err
			}
			defer service.Close()
			items, err := service.List(command.Context(), options.environment)
			if err != nil {
				return err
			}
			payload := struct {
				Flags []flags.Flag `json:"flags"`
			}{Flags: items}
			if err := writeOutput(command.OutOrStdout(), options.output, items, payload); err != nil {
				return errors.New("could not write command output")
			}
			return nil
		},
	}
}

func newGetCommand(options *options) *cobra.Command {
	return &cobra.Command{
		Use:     "get KEY",
		Short:   "Read one flag in an environment",
		Example: "  flagctl flags get checkout_v2 --env dev --output json",
		Args:    oneKeyArgument,
		RunE: func(command *cobra.Command, args []string) error {
			if err := flags.ValidateKey(args[0]); err != nil {
				return err
			}
			service, err := options.client(command)
			if err != nil {
				return err
			}
			defer service.Close()
			flag, err := service.Get(command.Context(), options.environment, args[0])
			if err != nil {
				return err
			}
			if err := writeOutput(command.OutOrStdout(), options.output, []flags.Flag{flag}, flag); err != nil {
				return errors.New("could not write command output")
			}
			return nil
		},
	}
}

func newToggleCommand(options *options) *cobra.Command {
	var enabled bool
	command := &cobra.Command{
		Use:     "toggle KEY --enabled=true|false",
		Short:   "Set a flag's enabled state explicitly",
		Long:    "Set a flag to the required target state. Repeating the same command preserves that state and leaves the description unchanged.",
		Example: "  flagctl flags toggle checkout_v2 --env dev --enabled=true\n  flagctl flags toggle checkout_v2 --env dev --enabled=false --output json",
		Args:    oneKeyArgument,
		RunE: func(command *cobra.Command, args []string) error {
			if err := flags.ValidateKey(args[0]); err != nil {
				return err
			}
			service, err := options.client(command)
			if err != nil {
				return err
			}
			defer service.Close()
			flag, err := service.Update(command.Context(), options.environment, args[0], flags.UpdateInput{Enabled: &enabled})
			if err != nil {
				return err
			}
			if err := writeOutput(command.OutOrStdout(), options.output, []flags.Flag{flag}, flag); err != nil {
				return errors.New("flag was updated, but output could not be written; use flags get to confirm")
			}
			return nil
		},
	}
	command.Flags().BoolVar(&enabled, "enabled", false, "required target state: --enabled=true or --enabled=false")
	// Require a value as well as the option, so a bare --enabled cannot mutate.
	command.Flags().Lookup("enabled").NoOptDefVal = ""
	if err := command.MarkFlagRequired("enabled"); err != nil {
		panic(err) // Programming error: the flag is registered just above.
	}
	return command
}

func newDeleteCommand(options *options) *cobra.Command {
	return &cobra.Command{
		Use:     "delete KEY",
		Short:   "Delete one flag from an environment",
		Example: "  flagctl flags delete checkout_v2 --env dev --output json",
		Args:    oneKeyArgument,
		RunE: func(command *cobra.Command, args []string) error {
			if err := flags.ValidateKey(args[0]); err != nil {
				return err
			}
			service, err := options.client(command)
			if err != nil {
				return err
			}
			defer service.Close()
			if err := service.Delete(command.Context(), options.environment, args[0]); err != nil {
				return err
			}
			if err := writeDeleteOutput(command.OutOrStdout(), options.output, options.environment, args[0]); err != nil {
				return errors.New("flag was deleted, but output could not be written; use flags list to confirm")
			}
			return nil
		},
	}
}

func oneKeyArgument(command *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("flags %s requires exactly one flag key", command.Name())
	}
	return nil
}
