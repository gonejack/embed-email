package embedemail

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/dustin/go-humanize"
	"github.com/gonejack/email"
	"github.com/gonejack/gex"
)

type EmbedEmail struct {
	Options
}

func (c *EmbedEmail) Run() (err error) {
	if len(c.Eml) == 0 {
		return errors.New("not .eml file found")
	}
	return c.run()
}
func (c *EmbedEmail) run() (err error) {
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

		if base := mail.Headers.Get("content-base"); base != "" {
			c.patchRef(base, doc)
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
func (c *EmbedEmail) saveMedia(doc *goquery.Document) map[string]media {
	saves := make(map[string]media)
	bat := gex.NewBatch(3)
	doc.Find("img,video,source").Each(func(i int, img *goquery.Selection) {
		src, _ := img.Attr("src")
		switch {
		case src == "":
			return
		case strings.HasPrefix(src, "data:"):
			return
		case strings.HasPrefix(src, "cid:"):
			return
		}
		if strings.HasPrefix(src, "http") {
			_ = os.MkdirAll(c.MediaDir, 0766)
			r := gex.NewRequest(c.MediaDir, src)
			r.SetTimeout(time.Minute * 5)
			r.SetHeader("user-agent", c.UserAgent)
			bat.Add(r)
		}
	})
	bat.OnStart(func(r *gex.Request) {
		if c.Verbose {
			log.Printf("download %s => %s", r.Url, r.Output)
		}
	})
	bat.OnStop(func(r *gex.Request, err error) {
		if err != nil {
			log.Printf("download %s as %s failed: %s", r.Url, r.Output, err)
			return
		}
		if c.Verbose {
			log.Printf("download %s as %s done", r.Url, r.Output)
		}
		saves[r.Url] = media{
			src:   r.Url,
			path:  r.Output,
			mime:  r.Response.Mime(),
			mtime: r.Response.ModTime(),
		}
	})
	bat.Run(nil)

	return saves
}
func (c *EmbedEmail) patchRef(base string, doc *goquery.Document) {
	b, err := url.Parse(base)
	if err != nil {
		return
	}
	doc.Find("img,video,source").Each(func(i int, e *goquery.Selection) {
		src, _ := e.Attr("src")
		switch {
		case src == "":
			return
		case strings.HasPrefix(src, "data:"):
			return
		case strings.HasPrefix(src, "cid:"):
			return
		case strings.HasPrefix(src, "http:"):
			return
		case strings.HasPrefix(src, "https:"):
			return
		default:
			u, err := url.Parse(src)
			if err != nil {
				return
			}
			if u.Scheme == "" {
				u.Scheme = b.Scheme
			}
			if u.Host == "" {
				u.Host = b.Host
			}
			e.SetAttr("src", u.String())
		}
	})
}
func (c *EmbedEmail) convertGif(doc *goquery.Document, saves map[string]media) {
	var once sync.Once
	doc.Find("img").Each(func(i int, img *goquery.Selection) {
		src, _ := img.Attr("src")
		if src == "" {
			return
		}
		u, e := url.Parse(src)
		if e != nil || path.Ext(u.Path) != ".gif" {
			return
		}
		_, e = exec.LookPath("ffmpeg")
		if e != nil {
			once.Do(func() { log.Printf("ffmpeg not installed, will not convert gif into mp4") })
			return
		}
		u.RawQuery = ""

		gif, exist := saves[src]
		if !exist {
			return
		}
		stat, e := os.Stat(gif.path)
		if e != nil || stat.Size() < 300*humanize.KiByte {
			return
		}

		mp4 := media{
			src:   u.String() + ".mp4",
			path:  gif.path + ".mp4",
			mime:  "video/mp4",
			mtime: gif.mtime,
		}

		// https://unix.stackexchange.com/questions/40638/how-to-do-i-convert-an-animated-gif-to-an-mp4-or-mv4-on-the-command-line
		c := exec.Command(
			"ffmpeg",
			"-y", // overwrite output
			"-i", gif.path,
			"-movflags", "faststart",
			"-pix_fmt", "yuv420p",
			"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
			mp4.path,
		)

		if e = c.Run(); e == nil {
			saves[mp4.src] = mp4
			tpl := `<video autoplay loop muted playsinline><source src="%s" type="video/mp4"></video>`
			img.ReplaceWithHtml(fmt.Sprintf(tpl, mp4.src))
		} else {
			log.Printf("convert %s => %s error: %s", gif.path, mp4.path, e)
		}
	})
}
func (c *EmbedEmail) changeRef(e *goquery.Selection, mail *email.Email, cids map[string]string, saves map[string]media) {
	e.RemoveAttr("loading")
	e.RemoveAttr("srcset")

	w, _ := e.Attr("width")
	if w == "0" {
		e.RemoveAttr("width")
	}
	h, _ := e.Attr("height")
	if h == "0" {
		e.RemoveAttr("height")
	}

	src, _ := e.Attr("src")
	switch {
	case src == "":
		return
	case strings.HasPrefix(src, "data:"):
		return
	case strings.HasPrefix(src, "cid:"):
		return
	case strings.HasPrefix(src, "http"):
		cid, exist := cids[src]
		if exist {
			e.SetAttr("src", fmt.Sprintf("cid:%s", cid))
			return
		}

		save, exist := saves[src]
		if !exist {
			log.Printf("missing local file of %s", src)
			return
		}
		if c.Verbose {
			log.Printf("replace %s as %s", src, save.path)
		}

		cid = save.path
		cids[src] = cid

		// add image
		f, err := os.Open(save.path)
		if err != nil {
			log.Printf("cannot open %s: %s", save.path, err)
			return
		}
		defer f.Close()

		hdr := make(textproto.MIMEHeader)
		{
			hdr.Set("last-modified", save.mtime.Format(http.TimeFormat))
			hdr.Set("content-location", src)
		}

		att, err := mail.AttachWithHeaders(f, cid, save.mime, hdr)
		if err != nil {
			log.Printf("cannot attach %s: %s", f.Name(), err)
			return
		}
		att.HTMLRelated = true

		e.SetAttr("src", fmt.Sprintf("cid:%s", cid))
	default:
		log.Printf("unsupported image reference[src=%s]", src)
	}
}

type media struct {
	src   string
	path  string
	mime  string
	mtime time.Time
}
