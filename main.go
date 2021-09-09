package main

import (
	"log"
	"os"

	"github.com/gonejack/embed-email/cmd"
)

func init() {
	log.SetOutput(os.Stdout)
}

func main() {
	c := cmd.EmbedEmail{
		MediaDir: "media",
	}
	err := c.Run()
	if err != nil {
		log.Fatal(err)
	}
}
