package cmd

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alecthomas/kong"
	"github.com/djherbis/times"
	"github.com/dustin/go-humanize"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gonejack/email"
	"github.com/gonejack/get"
)

type options struct {
	RetainGif bool `name:"retain-gif" help:"will not convert gif into mp4.'"`
	Verbose   bool `short:"v" help:"Verbose printing."`

	Eml []string `arg:"" optional:""`
}

type EmbedEmail struct {
	options
	MediaDir string
}

func (c *EmbedEmail) Run() (err error) {
	kong.Parse(&c.options,
		kong.Name("embed-email"),
		kong.Description("Command line tool for embed images within email."),
		kong.UsageOnError(),
	)
	if len(c.Eml) == 0 {
		c.Eml, _ = filepath.Glob("*.eml")
	}
	if len(c.Eml) == 0 {
		return errors.New("not .eml file found")
	}

	err = os.MkdirAll(c.MediaDir, 0777)
	if err != nil {
		return fmt.Errorf("cannot make dir %s", err)
	}

	return c.process()
}
func (c *EmbedEmail) process() (err error) {
	for _, eml := range c.Eml {
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

		saves := c.saveMedia(doc)
		cids := make(map[string]string)

		if !c.RetainGif {
			c.convertGif(doc, saves)
		}

		doc.Find("img,video,source").Each(func(i int, e *goquery.Selection) {
			c.changeRef(e, mail, cids, saves)
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

		err = os.WriteFile(embed, data, 0666)
		if err != nil {
			return fmt.Errorf("cannot write eml: %s", err)
		}
	}

	return
}
func (c *EmbedEmail) convertGif(doc *goquery.Document, media map[string]string) {
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Printf("ffmpeg not found, will not convert gif into mp4")
		return
	}

	doc.Find("img").Each(func(i int, img *goquery.Selection) {
		src, _ := img.Attr("src")
		if src == "" {
			return
		}

		u, err := url.Parse(src)
		if err != nil || !strings.HasSuffix(u.Path, ".gif") {
			return
		}
		u.RawQuery = ""

		gif, exist := media[src]
		if !exist {
			return
		}

		info, err := os.Stat(gif)
		if err != nil || info.Size() < 300*humanize.KiByte {
			return
		}

		mp4src := u.String() + ".mp4"
		mp4 := gif + ".mp4"

		// https://unix.stackexchange.com/questions/40638/how-to-do-i-convert-an-animated-gif-to-an-mp4-or-mv4-on-the-command-line
		cmd := exec.Command(
			"ffmpeg",
			"-y", // overwrite output
			"-i", gif,
			"-movflags", "faststart",
			"-pix_fmt", "yuv420p",
			"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
			mp4,
		)
		err = cmd.Run()
		if err != nil {
			log.Printf("convert %s => %s error: %s", gif, mp4, err)
			return
		}

		tpl := `<video autoplay loop muted playsinline><source src="%s" type="video/mp4"></video>`
		media[mp4src] = mp4
		img.ReplaceWithHtml(fmt.Sprintf(tpl, mp4src))
	})
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
	tasks := get.NewDownloadTasks()

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

		downloads[src] = localpath
		tasks.Add(src, localpath)
	})

	g := get.Default()
	g.OnEachStart = func(t *get.DownloadTask) {
		if c.Verbose {
			log.Printf("download %s => %s", t.Link, t.Path)
		}
	}
	g.OnEachStop = func(t *get.DownloadTask) {
		if t.Err != nil {
			log.Printf("download %s as %s failed: %s", t.Link, t.Path, t.Err)
			return
		}
		if c.Verbose {
			log.Printf("download %s as %s done", t.Link, t.Path)
		}
	}
	g.Batch(tasks, 3, time.Minute*2)

	return downloads
}
func (c *EmbedEmail) changeRef(e *goquery.Selection, mail *email.Email, mediaCIDs, mediaFiles map[string]string) {
	e.RemoveAttr("loading")
	e.RemoveAttr("srcset")

	src, _ := e.Attr("src")
	switch {
	case src == "":
		return
	case strings.HasPrefix(src, "data:"):
		return
	case strings.HasPrefix(src, "cid:"):
		return
	case strings.HasPrefix(src, "http"):
		mediaCID, exist := mediaCIDs[src]
		if exist {
			e.SetAttr("src", fmt.Sprintf("cid:%s", mediaCID))
			return
		}

		mediaFile := mediaFiles[src]
		if c.Verbose {
			log.Printf("replace %s as %s", src, mediaFile)
		}

		// check mime
		fmime, err := mimetype.DetectFile(mediaFile)
		switch {
		case err != nil:
			log.Printf("cannot detect mime of %s: %s", src, err)
			return
		case strings.HasPrefix(fmime.String(), "video"):
		case strings.HasPrefix(fmime.String(), "image"):
		default:
			log.Printf("mime of %s is %s instead of images", src, fmime.String())
			return
		}

		// read time
		header := make(textproto.MIMEHeader)
		if s, e := times.Stat(mediaFile); e == nil {
			t := s.ModTime()
			if s.HasBirthTime() {
				t = s.BirthTime()
			}
			header.Set("last-modified", t.Format(http.TimeFormat))
		}

		// add image
		reader, err := os.Open(mediaFile)
		if err != nil {
			log.Printf("cannot open %s: %s", mediaFile, err)
			return
		}
		defer reader.Close()

		mediaCID = md5str(src) + fmime.Extension()
		mediaCIDs[src] = mediaCID

		attachment, err := mail.AttachWithHeaders(reader, mediaCID, fmime.String(), header)
		if err != nil {
			log.Printf("cannot attach %s: %s", reader.Name(), err)
			return
		}
		attachment.HTMLRelated = true

		e.SetAttr("src", fmt.Sprintf("cid:%s", mediaCID))
	default:
		log.Printf("unsupported image reference[src=%s]", src)
	}
}

func md5str(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
