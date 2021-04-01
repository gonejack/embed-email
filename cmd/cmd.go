package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/dustin/go-humanize"
	"github.com/gabriel-vasile/mimetype"
	"github.com/jordan-wright/email"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type EmbedEmail struct {
	client http.Client

	ImagesDir string

	Verbose bool
}

func (c *EmbedEmail) Execute(emails []string) (err error) {
	if len(emails) == 0 {
		emails, _ = filepath.Glob("*.eml")
	}
	if len(emails) == 0 {
		return errors.New("no eml given")
	}

	err = c.mkdirs()
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

		downloads := c.downloadImages(document)

		src2cid := make(map[string]string)
		document.Find("img").Each(func(i int, img *goquery.Selection) {
			c.changeRef(img, mail, src2cid, downloads)
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
func (c *EmbedEmail) downloadImages(doc *goquery.Document) map[string]string {
	downloads := make(map[string]string)
	downloadLinks := make([]string, 0)
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

		downloads[src] = localFile
		downloadLinks = append(downloadLinks, src)
	})

	var batch = semaphore.NewWeighted(3)
	var group errgroup.Group

	for i := range downloadLinks {
		_ = batch.Acquire(context.TODO(), 1)

		src := downloadLinks[i]
		group.Go(func() error {
			defer batch.Release(1)

			if c.Verbose {
				log.Printf("fetch %s", src)
			}

			err := c.download(downloads[src], src)
			if err != nil {
				log.Printf("download %s fail: %s", src, err)
			}

			return nil
		})
	}

	_ = group.Wait()

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
func (c *EmbedEmail) download(path string, src string) (err error) {
	timeout, cancel := context.WithTimeout(context.TODO(), time.Minute*2)
	defer cancel()

	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		headReq, headErr := http.NewRequestWithContext(timeout, http.MethodHead, src, nil)
		if headErr != nil {
			return headErr
		}
		resp, headErr := c.client.Do(headReq)
		if headErr == nil && info.Size() == resp.ContentLength {
			return // skip download
		}
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return
	}
	defer file.Close()

	request, err := http.NewRequestWithContext(timeout, http.MethodGet, src, nil)
	if err != nil {
		return
	}
	response, err := c.client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()

	var written int64
	if c.Verbose {
		bar := progressbar.NewOptions64(response.ContentLength,
			progressbar.OptionSetTheme(progressbar.Theme{Saucer: "=", SaucerPadding: ".", BarStart: "|", BarEnd: "|"}),
			progressbar.OptionSetWidth(10),
			progressbar.OptionSpinnerType(11),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionSetPredictTime(false),
			progressbar.OptionSetDescription(filepath.Base(src)),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionClearOnFinish(),
		)
		defer bar.Clear()
		written, err = io.Copy(io.MultiWriter(file, bar), response.Body)
	} else {
		written, err = io.Copy(file, response.Body)
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("response status code %d invalid", response.StatusCode)
	}

	if err == nil && written < response.ContentLength {
		err = fmt.Errorf("expected %s but downloaded %s", humanize.Bytes(uint64(response.ContentLength)), humanize.Bytes(uint64(written)))
	}

	return
}
func (c *EmbedEmail) mkdirs() error {
	err := os.MkdirAll(c.ImagesDir, 0777)
	if err != nil {
		return fmt.Errorf("cannot make images dir %s", err)
	}

	return nil
}

func md5str(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
