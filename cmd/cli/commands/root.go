package commands

import (
	"os"

	"github.com/Noah-Wilderom/dfs/pkg/logging"
	"github.com/spf13/cobra"
)

var (
	logger  = logging.MustNew()
	rootCmd = &cobra.Command{
		Use:   "dfs",
		Short: "Distributed File System",
		Long: `P2P File System
			long description...`,
	}
)

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	//
}
