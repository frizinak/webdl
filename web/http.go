package web

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var multispaceRE = regexp.MustCompile(`\s+`)

func (w *Web) get(ctx context.Context, p PageInfo) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.URL.String(), nil)
	if err != nil {
		return nil, err
	}
	if p.Ref != nil {
		req.Header.Set("referer", p.Ref.URL.String())
	}
	res, err := w.c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (w *Web) page(ctx context.Context, pi PageInfo, s Selectors) (*Page, error) {
	r, err := w.get(ctx, pi)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(r)
	r.Close()
	if err != nil {
		return nil, err
	}

	addLink := func(list *[]*url.URL) func(int, string) {
		return func(qix int, data string) {
			href, err := HREF(pi.URL, data)
			if err != nil {
				return
			}
			*list = append(*list, href)
		}
	}

	qry := func(q []Selector, add func(int, string)) {
		for qix, qry := range q {
			doc.Find(qry.Query).Each(func(i int, s *goquery.Selection) {
				if qry.Attr == "" {
					add(qix, s.Text())
					return
				}

				if val, ok := s.Attr(qry.Attr); ok {
					add(qix, val)
				}
			})
		}
	}

	p := newPage(pi)
	qry(s.Links, addLink(&p.Links))
	qry(s.Downloads, addLink(&p.Downloads))
	qry(s.Titles, func(qix int, data string) {
		if p.Title == "" {
			p.Title = strings.TrimSpace(multispaceRE.ReplaceAllString(data, " "))
		}
	})

	entries := make(map[int]int)
	qry(s.Prints, func(qix int, data string) {
		entries[qix]++
		if len(p.Prints) < entries[qix] {
			p.Prints = append(p.Prints, make([]string, len(s.Prints)))
		}
		p.Prints[entries[qix]-1][qix] = data
	})

	return p, nil
}

func (w *Web) download(ctx context.Context, p PageInfo, cb DownloadCallback) error {
	r, err := w.get(ctx, p)
	if err != nil {
		return err
	}
	defer r.Close()

	return cb(p, r)
}
