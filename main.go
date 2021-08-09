package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gonejack/embed-email/cmd"
)

var (
	convertGIF bool
	verbose    bool

	prog = &cobra.Command{
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
	pfs := prog.PersistentFlags()
	pfs.SortFlags = false
	pfs.BoolVarP(&convertGIF, "convert-gif", "", true, "convert gif to mp4 using ffmpeg, --convert-gif=false to disable")
	pfs.BoolVarP(&verbose, "verbose", "v", false, "verbose")
}

func run(c *cobra.Command, args []string) error {
	exec := cmd.EmbedEmail{
		MediaDir:   "media",
		ConvertGIF: convertGIF,
		Verbose:    verbose,
	}

	if len(args) == 0 {
		args, _ = filepath.Glob("*.eml")
	}

	return exec.Execute(args)
}

func main() {
	_ = prog.Execute()
}
