package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/frizinak/webdl/web"
)

type flagStrs []string

func (i *flagStrs) String() string {
	return "my string representation"
}

func (i *flagStrs) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type TplData struct {
	URL     string
	Referer string

	Index     int
	PageIndex int

	Title string
	Name  string
	Ext   string
	Data  [][]string
}

func main() {
	var linksQ flagStrs
	var downloadsQ flagStrs
	var printsQ flagStrs
	var titleQ flagStrs
	var dir string
	var downloadFormat string
	var printFormat string
	var numRevPages bool
	var numRevDownloads bool
	var dry bool
	var noprogress bool
	var concurrency int
	flag.BoolVar(&dry, "n", false, "dry run and print what would be downloaded (http requests will still be made for pages)")
	flag.BoolVar(&noprogress, "np", false, "no progress")

	flag.Var(&linksQ, "sl", "selector for links (can be specified multiple times)")
	flag.Var(&downloadsQ, "sd", "selector for downloads (can be specified multiple times)")
	flag.Var(&printsQ, "sp", "selector for printing to stdout (can be specified multiple times)")
	flag.Var(&titleQ, "st", "selector for title (can be specified multiple times)")

	flag.BoolVar(&numRevPages, "rl", false, "{{ .PageIndex }} in -f will be the inverse")
	flag.BoolVar(&numRevDownloads, "rd", false, "{{ .Index }} in -f will be the inverse")

	flag.IntVar(&concurrency, "c", 8, "download concurrency")

	flag.StringVar(&dir, "d", ".", "Destination directory")
	defFormat := filepath.Join(
		`{{ printf "%06d" .PageIndex }} - {{ .Title | alphanum }}`,
		`{{ printf "%06d" .Index }} - {{ .Name | alphanum }}.{{ .Ext | alphanum }}`,
	)
	flag.StringVar(
		&downloadFormat,
		"df",
		defFormat,
		`golang template that expands to the relative filepath of each download
		Note!: do not forget to pass any untrusted data through 'alphanum' or 'path'
		       (untrusted: .Title, .URL .Referer .Name and .Ext)

available fields: ('page' here refers to the page the current download was found on)
  - .URL         url of the download.
  - .Referer     url of the page.
  - .Index:      index of the download within the page.
  - .PageIndex:  index of the page within the list of found pages.
  - .Title:      title of the page.
  - .Name:       filename from url without extension
  - .Ext:        file extension from url without leading '.'

available functions:
  - printf format ...args: well printf
  - alphanum:              replaces common unsafe characters with a '-'
  - path:                  less strict than alphanum, just remove path separators.
  - nl:                    a newline
  - tab:                   a tab
  - href base_url href:    turn a relative url into an absolute one
						   {{ range $entry := .Data }}{{ range $k, $v := $entry }}{{ $k }}{{tab}}{{ href .URL $v }}{{nl}}{{ end }}{{ end }}
                           e.g.: {{ href .URL $v }}

`,
	)
	flag.StringVar(
		&printFormat,
		"pf",
		"{{ range .Data }}{{ range $k, $v := . }}{{ $k }}{{tab}}{{ $v }}{{nl}}{{ end }}{{ end }}",
		`golang template used for data matched with -sp

available fields:
  - .Data:       a 2-dimensional array of items matched with -sp
  - .URL         url of the current page.
  - .Referer     url of the referer.
  - .PageIndex:  index of the page within the list of found pages.
  - .Title:      title of the page.

available functions: see -df

`,
	)
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(out, "%s [flags] ...urls\n", os.Args[0])
		fmt.Fprintln(out)
		flag.PrintDefaults()
		fmt.Fprintln(out)
		fmt.Fprintln(out, "These are equivalent:")
		fmt.Fprintln(out, "-l '.content a[href], .footer a[href]'")
		fmt.Fprintln(out, "-l '.content a[href]' -l '.footer a[href]'")
	}
	flag.Parse()

	var cancelErr error
	ctx, ctxCancel := context.WithCancel(context.Background())

	conf := web.Config{
		UserAgent:   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36",
		Concurrency: concurrency,
	}

	w := web.New(conf)

	pageToTpl := func(p web.PageInfo) TplData {
		fn := strings.TrimSpace(path.Base(p.URL.String()))
		ext := path.Ext(fn)
		if len(ext) > 0 && ext[0] == '.' {
			ext = ext[1:]
		}
		refurl, reftitle, refindex := "", "", 0
		if p.Ref != nil {
			if p.Ref.URL != nil {
				refurl = p.Ref.URL.String()
			}
			reftitle = p.Ref.Title
			refindex = p.Ref.Index
		}
		return TplData{
			URL:       p.URL.String(),
			Referer:   refurl,
			Index:     p.Index,
			PageIndex: refindex,
			Title:     strings.TrimSpace(reftitle),
			Name:      fn[:len(fn)-len(ext)],
			Ext:       ext,
		}
	}

	alphanumRE := regexp.MustCompile(`(?i)[^a-z0-9\-_ ]+`)
	slashRE := regexp.MustCompile(`^\.\.+|\\+|\/+|\.\.+$`)
	tplFuncs := template.FuncMap{
		"href": func(base, href string) string {
			u, err := url.Parse(base)
			if err != nil {
				u, _ = url.Parse("https://invalid-base.url")
			}
			uri, err := web.HREF(u, href)
			if err != nil {
				return "https://invalid-href.url"
			}
			return uri.String()
		},
		"printf": func(format string, s ...interface{}) string {
			return fmt.Sprintf(format, s...)
		},
		"alphanum": func(s string) string {
			return strings.Trim(alphanumRE.ReplaceAllString(s, "-"), "-")
		},
		"path": func(s string) string {
			return strings.Trim(slashRE.ReplaceAllString(s, "-"), "-")
		},
		"nl":  func() string { return "\n" },
		"tab": func() string { return "\t" },
	}
	downloadTpl, err := template.New("downloadFormat").
		Funcs(tplFuncs).
		Parse(downloadFormat)

	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid download template: %s\n", err.Error())
		os.Exit(1)
	}

	printTpl, err := template.New("printFormat").
		Funcs(tplFuncs).
		Parse(printFormat)

	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid print template: %s\n", err.Error())
		os.Exit(1)
	}

	doDownloadTpl := func(p web.PageInfo) string {
		buf := bytes.NewBuffer(nil)
		err := downloadTpl.Execute(buf, pageToTpl(p))
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid download template: %s\n", err.Error())
			cancelErr = err
			ctxCancel()
		}

		return buf.String()
	}

	rc := web.RecursiveConfig{
		URLs:             flag.Args(),
		ProgressInterval: time.Millisecond * 50,
		ReverseLinks:     numRevPages,
		ReverseDownloads: numRevDownloads,
		Selectors: web.Selectors{
			Links:     web.NewSelectors(linksQ),
			Downloads: web.NewSelectors(downloadsQ),
			Prints:    web.NewSelectors(printsQ),
			Titles:    web.NewSelectors(titleQ),
		},
		Progress: func(err error, i, n uint64) {
			if err != nil {
				fmt.Fprintln(os.Stderr, "\n", err)
				return
			}
			if noprogress {
				return
			}

			p := int(100 * float64(i) / float64(n))
			if p < 0 {
				p = 0
			}
			if dry {
				fmt.Fprintf(os.Stderr, "%d/%d [%d%%]\n", i, n, p)
				return
			}
			fmt.Fprintf(os.Stderr, "\033[30D\033[K%d/%d [%d%%]", i, n, p)
		},
		PrintCallback: func(p web.PageInfo, data [][]string) {
			pd := pageToTpl(p)
			pd.Data = data
			err := printTpl.Execute(os.Stdout, pd)
			if err != nil {
				cancelErr = err
				ctxCancel()
			}
		},
		Download: func(p web.PageInfo) bool {
			dest := filepath.Join(dir, doDownloadTpl(p))
			_, err := os.Stat(dest)
			dl := err != nil && os.IsNotExist(err)
			if dl && dry {
				refurl := ""
				if p.Ref != nil && p.Ref.URL != nil {
					refurl = p.Ref.URL.String()
				}
				fmt.Printf(
					"Page: %s\nDownload: %s\nDest:%s\n",
					refurl,
					p.URL.String(),
					dest,
				)
				return false
			}
			return dl
		},
		DownloadCallback: func(p web.PageInfo, r io.Reader) error {
			dest := filepath.Join(dir, doDownloadTpl(p))
			stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
			rnd := make([]byte, 32)
			_, err := io.ReadFull(rand.Reader, rnd)
			if err != nil {
				return err
			}

			tmp := fmt.Sprintf(
				"%s.%s-%s.webdl.tmp",
				dest,
				stamp,
				base64.RawURLEncoding.EncodeToString(rnd),
			)
			os.MkdirAll(filepath.Dir(dest), 0750)

			f, err := os.Create(tmp)
			if err != nil {
				return err
			}

			_, err = io.Copy(f, r)
			f.Close()
			if err != nil {
				os.Remove(tmp)
				return err
			}

			return os.Rename(tmp, dest)
		},
	}

	cleanup := func() {
		filepath.WalkDir(dir, func(path string, info fs.DirEntry, err error) error {
			if strings.HasSuffix(path, ".webl.tmp") {
				os.Remove(path)
			}
			return nil
		})
	}
	defer cleanup()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sig
		cleanup()
		os.Exit(1)
	}()

	err = w.Recurse(ctx, rc)
	if err != nil {
		if cancelErr != nil {
			err = cancelErr
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
