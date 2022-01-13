package embedemail

import "github.com/alecthomas/kong"

type Options struct {
	RetainGif bool   `name:"retain-gif" help:"Will not convert gif into mp4."`
	Verbose   bool   `short:"v" help:"Verbose printing."`
	About     bool   `help:"About."`
	MediaDir  string `hidden:"" default:"media"`

	Eml []string `name:".eml" arg:"" optional:"" help:"list of .eml files"`
}

func MustParseOptions() (opts Options) {
	kong.Parse(&opts,
		kong.Name("embed-email"),
		kong.Description("This command parse email content, download remote images and replace them as inline images"),
		kong.UsageOnError(),
	)
	return
}
