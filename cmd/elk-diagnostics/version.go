package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "印工具版本",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("elk-diagnostics", toolVersion)
			return nil
		},
	}
}
