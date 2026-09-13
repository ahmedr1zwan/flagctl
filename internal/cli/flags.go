package cli

import (
	"errors"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/spf13/cobra"
)

func newCreateCommand(options *options) *cobra.Command {
	var input flags.CreateInput
	command := &cobra.Command{
		Use:     "create KEY",
		Short:   "Create a flag, disabled by default",
		Example: "  flagctl flags create checkout_v2 --env dev --description \"New checkout\"\n  flagctl flags create checkout_v2 --env prod --enabled=true --output json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("flags create requires exactly one flag key")
			}
			return nil
		},
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
