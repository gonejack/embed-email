package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gonejack/embed-email/cmd"
	"github.com/spf13/cobra"
)

var (
	verbose = false
	prog    = &cobra.Command{
		Use:   "embed-email *.eml",
		Short: "Command line tool for embed images within email.",
		Run: func(c *cobra.Command, args []string) {
			err := run(c, args)
			if err != nil {
				log.Fatal(err)
			}
		},
	}
)

func init() {
	log.SetOutput(os.Stdout)

	prog.Flags().SortFlags = false
	prog.PersistentFlags().SortFlags = false
	prog.PersistentFlags().BoolVarP(
		&verbose,
		"verbose",
		"v",
		false,
		"verbose",
	)
}

func run(c *cobra.Command, args []string) error {
	exec := cmd.EmbedEmail{
		ImagesDir: "images",
		Verbose:   verbose,
	}

	if len(args) == 0 {
		args, _ = filepath.Glob("*.eml")
	}

	return exec.Execute(args)
}

func main() {
	_ = prog.Execute()
}
