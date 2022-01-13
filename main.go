package main

import (
	"log"
	"os"

	"github.com/gonejack/embed-email/embedemail"
)

func init() {
	log.SetOutput(os.Stdout)
}

func main() {
	cmd := embedemail.EmbedEmail{
		Options: embedemail.MustParseOptions(),
	}
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}
