package cmd

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gonejack/email"
	"github.com/gonejack/get"
)

type EmbedEmail struct {
	MediaDir string
	Verbose  bool
}

func (c *EmbedEmail) Execute(emails []string) (err error) {
	if len(emails) == 0 {
		return errors.New("no eml given")
	}

	err = c.mkdir()
	if err != nil {
		return
	}

	for _, eml := range emails {
		if strings.HasSuffix(eml, ".embed.eml") {
			if c.Verbose {
				log.Printf("skipped %s", eml)
			}
			continue
		}

		embed := strings.TrimSuffix(eml, ".eml") + ".embed.eml"
		if _, e := os.Stat(embed); !errors.Is(e, fs.ErrNotExist) {
			if c.Verbose {
				log.Printf("skipped %s: expected .embed.eml file already exist", eml)
			}
			continue
		}

		log.Printf("process %s", eml)
		mail, err := c.openEmail(eml)
		if err != nil {
			return err
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(mail.HTML))
		if err != nil {
			return fmt.Errorf("cannot parse HTML: %s", err)
		}

		imgFiles := c.saveMedia(doc)
		imgCIDs := make(map[string]string)
		doc.Find("img,video").Each(func(i int, img *goquery.Selection) {
			c.changeRef(img, mail, imgCIDs, imgFiles)
		})

		html, err := doc.Html()
		if err != nil {
			return fmt.Errorf("cannot generate html: %s", err)
		}
		mail.HTML = []byte(html)

		data, err := mail.Bytes()
		if err != nil {
			return fmt.Errorf("cannot generate eml: %s", err)
		}

		err = ioutil.WriteFile(embed, data, 0766)
		if err != nil {
			return fmt.Errorf("cannot write eml: %s", err)
		}
	}

	return
}

func (c *EmbedEmail) openEmail(eml string) (*email.Email, error) {
	file, err := os.Open(eml)
	if err != nil {
		return nil, fmt.Errorf("cannot open file: %s", err)
	}
	defer file.Close()
	mail, err := email.NewEmailFromReader(file)
	if err != nil {
		return nil, fmt.Errorf("cannot parse email: %s", err)
	}
	return mail, nil
}
func (c *EmbedEmail) saveMedia(doc *goquery.Document) map[string]string {
	downloads := make(map[string]string)

	var refs, paths []string
	doc.Find("img,video").Each(func(i int, img *goquery.Selection) {
		src, _ := img.Attr("src")

		if !strings.HasPrefix(src, "http") {
			return
		}

		localpath, exist := downloads[src]
		if exist {
			return
		}

		uri, err := url.Parse(src)
		if err != nil {
			log.Printf("parse %s fail: %s", src, err)
			return
		}
		localpath = filepath.Join(c.MediaDir, fmt.Sprintf("%s%s", md5str(src), filepath.Ext(uri.Path)))

		refs = append(refs, src)
		paths = append(paths, localpath)

		downloads[src] = localpath
	})

	getter := get.DefaultGetter()
	getter.Verbose = c.Verbose
	getter.AfterDL = func(ref string, path string, err error) {
		if err == nil {
			if c.Verbose {
				log.Printf("%s as %s done", ref, path)
			}
		} else {
			log.Printf("%s as %s failed: %s", ref, path, err)
		}
	}
	getter.BatchInOrder(refs, paths, 3, time.Minute*2)

	return downloads
}
func (c *EmbedEmail) changeRef(img *goquery.Selection, mail *email.Email, mediaCIDs, mediaFiles map[string]string) {
	img.RemoveAttr("loading")
	img.RemoveAttr("srcset")

	src, _ := img.Attr("src")
	switch {
	case strings.HasPrefix(src, "data:"):
		return
	case strings.HasPrefix(src, "cid:"):
		return
	case strings.HasPrefix(src, "http"):
		mediaCID, exist := mediaCIDs[src]
		if exist {
			img.SetAttr("src", fmt.Sprintf("cid:%s", mediaCID))
			return
		}

		mediaFile := mediaFiles[src]
		if c.Verbose {
			log.Printf("replace %s as %s", src, mediaFile)
		}

		// check mime
		mediaMIME, err := mimetype.DetectFile(mediaFile)
		switch {
		case err != nil:
			log.Printf("cannot detect mime of %s: %s", src, err)
			return
		case strings.HasPrefix(mediaMIME.String(), "video"):
		case strings.HasPrefix(mediaMIME.String(), "image"):
		default:
			log.Printf("mime of %s is %s instead of images", src, mediaMIME.String())
			return
		}

		// add image
		mediaReader, err := os.Open(mediaFile)
		if err != nil {
			log.Printf("cannot open %s: %s", mediaFile, err)
			return
		}
		defer mediaReader.Close()

		mediaCID = md5str(src) + mediaMIME.Extension()
		mediaCIDs[src] = mediaCID

		attachment, err := mail.Attach(mediaReader, mediaCID, mediaMIME.String())
		if err != nil {
			log.Printf("cannot attach %s: %s", mediaReader.Name(), err)
			return
		}
		attachment.HTMLRelated = true

		img.SetAttr("src", fmt.Sprintf("cid:%s", mediaCID))
	default:
		log.Printf("unsupported image reference[src=%s]", src)
	}
}
func (c *EmbedEmail) mkdir() error {
	err := os.MkdirAll(c.MediaDir, 0777)
	if err != nil {
		return fmt.Errorf("cannot make images dir %s", err)
	}

	return nil
}

func md5str(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
