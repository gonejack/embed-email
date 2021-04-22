package cmd

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
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
	ImagesDir string
	Verbose   bool
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
		log.Printf("procssing %s", eml)

		mail, err := c.openEmail(eml)
		if err != nil {
			return err
		}

		document, err := goquery.NewDocumentFromReader(bytes.NewReader(mail.HTML))
		if err != nil {
			return fmt.Errorf("cannot parse HTML: %s", err)
		}

		saved := c.saveImages(document)
		ref2cid := make(map[string]string)
		document.Find("img").Each(func(i int, img *goquery.Selection) {
			c.changeRef(img, mail, ref2cid, saved)
		})

		htm, err := document.Html()
		if err != nil {
			return fmt.Errorf("cannot generate html: %s", err)
		}

		mail.HTML = []byte(htm)
		data, err := mail.Bytes()
		if err != nil {
			return fmt.Errorf("cannot generate eml: %s", err)
		}

		target := strings.TrimSuffix(eml, ".eml") + ".embed.eml"
		err = ioutil.WriteFile(target, data, 0766)
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
func (c *EmbedEmail) saveImages(doc *goquery.Document) map[string]string {
	downloads := make(map[string]string)

	var refs, paths []string
	doc.Find("img").Each(func(i int, img *goquery.Selection) {
		src, _ := img.Attr("src")
		if !strings.HasPrefix(src, "http") {
			return
		}

		localFile, exist := downloads[src]
		if exist {
			return
		}

		uri, err := url.Parse(src)
		if err != nil {
			log.Printf("parse %s fail: %s", src, err)
			return
		}
		localFile = filepath.Join(c.ImagesDir, fmt.Sprintf("%s%s", md5str(src), filepath.Ext(uri.Path)))

		refs = append(refs, src)
		paths = append(paths, localFile)
		downloads[src] = localFile
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
func (c *EmbedEmail) changeRef(img *goquery.Selection, mail *email.Email, src2cid, downloads map[string]string) {
	img.RemoveAttr("loading")
	img.RemoveAttr("srcset")

	src, _ := img.Attr("src")
	switch {
	case strings.HasPrefix(src, "data:"):
		return
	case strings.HasPrefix(src, "cid:"):
		return
	case strings.HasPrefix(src, "http"):
		cid, exist := src2cid[src]
		if exist {
			img.SetAttr("src", fmt.Sprintf("cid:%s", cid))
			return
		}

		localFile := downloads[src]
		if c.Verbose {
			log.Printf("replace %s as %s", src, localFile)
		}

		// check mime
		fmime, err := mimetype.DetectFile(localFile)
		if err != nil {
			log.Printf("cannot detect image mime of %s: %s", src, err)
			return
		}
		if !strings.HasPrefix(fmime.String(), "image") {
			log.Printf("mime of %s is %s instead of images", src, fmime.String())
			return
		}

		// add image
		fd, err := os.Open(localFile)
		if err != nil {
			log.Printf("cannot open %s: %s", localFile, err)
		}
		defer fd.Close()

		cid = md5str(src) + fmime.Extension()
		{
			src2cid[src] = cid
		}

		attachment, err := mail.Attach(fd, cid, fmime.String())
		if err != nil {
			log.Printf("cannot attach %s: %s", fd.Name(), err)
			return
		}
		attachment.HTMLRelated = true

		img.SetAttr("src", fmt.Sprintf("cid:%s", cid))
	default:
		log.Printf("unsupported image reference[src=%s]", src)
	}
}
func (c *EmbedEmail) mkdir() error {
	err := os.MkdirAll(c.ImagesDir, 0777)
	if err != nil {
		return fmt.Errorf("cannot make images dir %s", err)
	}

	return nil
}

func md5str(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
