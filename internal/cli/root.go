// Package cli defines the Cobra command tree and user-facing output.
package cli

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/spf13/cobra"
)

type options struct {
	server      string
	tokenFile   string
	caFile      string
	timeout     time.Duration
	output      string
	environment string
}

func NewCommand(stdout, stderr io.Writer) *cobra.Command {
	options := &options{}
	root := &cobra.Command{
		Use:           "flagctl",
		Short:         "Manage environment-scoped feature flags",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error {
		return errors.New("invalid or unknown option; use --help for available options and types")
	})
	root.PersistentFlags().StringVar(&options.server, "server", client.DefaultServer, "service origin (overrides FLAGCTL_SERVER)")
	root.PersistentFlags().StringVar(&options.tokenFile, "token-file", "", "private bearer token file (overrides FLAGCTL_TOKEN_FILE; HTTPS only)")
	root.PersistentFlags().StringVar(&options.caFile, "ca-file", "", "PEM CA bundle (overrides FLAGCTL_CA_FILE; defaults to system trust)")
	root.PersistentFlags().DurationVar(&options.timeout, "timeout", 10*time.Second, "maximum time for the HTTP request")
	root.PersistentFlags().StringVarP(&options.output, "output", "o", "table", "output format: table or json")
	// Keep help focused on the implemented commands for this increment.
	root.CompletionOptions.DisableDefaultCmd = true
	flagCommands := &cobra.Command{
		Use:   "flags",
		Short: "Create, list, get, toggle, and delete flags across environments",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("choose a supported flag command: create, list, get, toggle, or delete; use flags --help")
		},
	}
	flagCommands.PersistentFlags().StringVar(&options.environment, "env", "", "environment name (required)")
	if err := flagCommands.MarkPersistentFlagRequired("env"); err != nil {
		panic(err) // Programming error: the flag is registered just above.
	}
	flagCommands.AddCommand(newCreateCommand(options), newListCommand(options), newGetCommand(options), newToggleCommand(options), newDeleteCommand(options))
	root.AddCommand(flagCommands)
	return root
}

func (options *options) client(command *cobra.Command) (*client.Client, error) {
	if err := flags.ValidateEnvironment(options.environment); err != nil {
		return nil, err
	}
	if options.output != "table" && options.output != "json" {
		return nil, errors.New("--output must be table or json")
	}
	server := options.server
	// Resolve the environment at execution time so help never exposes its value.
	if !command.Flags().Changed("server") {
		if value := os.Getenv("FLAGCTL_SERVER"); value != "" {
			server = value
		}
	}
	tokenFile, caFile := options.tokenFile, options.caFile
	if !command.Flags().Changed("token-file") {
		tokenFile = os.Getenv("FLAGCTL_TOKEN_FILE")
	}
	if !command.Flags().Changed("ca-file") {
		caFile = os.Getenv("FLAGCTL_CA_FILE")
	}
	return client.NewWithConfig(client.Config{Server: server, Timeout: options.timeout, TokenFile: tokenFile, CAFile: caFile})
}
