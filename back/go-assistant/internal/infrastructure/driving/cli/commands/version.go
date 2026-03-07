package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			info := version.Get()
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "liwaisi %s\n", info.Version)
			fmt.Fprintf(w, "commit  %s\n", info.GitCommit)
			fmt.Fprintf(w, "built   %s\n", info.BuildTime)
			fmt.Fprintf(w, "go      %s\n", info.GoVersion)
			fmt.Fprintf(w, "os      %s/%s\n", info.OS, info.Arch)
		},
	}
}
