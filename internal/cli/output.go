package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

func writeOutput(writer io.Writer, format string, items []flags.Flag, payload any) error {
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "ENVIRONMENT\tKEY\tENABLED\tDESCRIPTION"); err != nil {
		return err
	}
	for _, flag := range items {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%t\t%s\n", tableCell(flag.Environment), tableCell(flag.Key), flag.Enabled, tableCell(flag.Description)); err != nil {
			return err
		}
	}
	return table.Flush()
}

// Escape terminal controls and embedded newlines so descriptions cannot execute
// terminal escape sequences or forge additional table rows.
func tableCell(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}
