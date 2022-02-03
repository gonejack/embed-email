package embedemail

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/alecthomas/kong"
)

type about bool

func (a about) BeforeApply() (err error) {
	fmt.Println("Visit https://github.com/gonejack/embed-email")
	os.Exit(0)
	return
}

type Options struct {
	RetainGif bool     `name:"retain-gif" help:"Will not convert gif into mp4."`
	Verbose   bool     `short:"v" help:"Verbose printing."`
	MediaDir  string   `hidden:"" default:"media"`
	About     about    `help:"About."`
	Eml       []string `name:".eml" arg:"" optional:"" help:"list of .eml files"`
}

func MustParseOptions() (opts Options) {
	kong.Parse(&opts,
		kong.Name("embed-email"),
		kong.Description("This command parse email content, download remote images and replace them as inline images"),
		kong.UsageOnError(),
	)
	if len(opts.Eml) == 0 || (runtime.GOOS == "windows" && opts.Eml[0] == "*.eml") {
		opts.Eml, _ = filepath.Glob("*.eml")
	}
	return
}
