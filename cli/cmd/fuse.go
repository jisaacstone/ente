package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var fuseCmd = &cobra.Command{
	Use:   "fuse",
	Short: "Prints the current version",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version %s\n", version)
		ctrl.Mount()
	},
}

func init() {
	rootCmd.AddCommand(fuseCmd)
}
