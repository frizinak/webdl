package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type Web struct {
	c Config
}

type Config struct {
	Client      *http.Client
	UserAgent   string
	Concurrency int
}

func New(c Config) *Web {
	if c.Client == nil {
		c.Client = http.DefaultClient
	}

	if c.Concurrency <= 0 {
		c.Concurrency = 8
	}

	return &Web{c}
}

type PageInfo struct {
	URL   *url.URL
	Ref   *PageInfo
	Title string
	Index int
}

type Page struct {
	PageInfo
	Links     []*url.URL
	Downloads []*url.URL
	Prints    [][]string
}

type pageTask struct {
	PageInfo
	Download bool
}

func newPage(p PageInfo) *Page {
	return &Page{
		PageInfo:  p,
		Links:     make([]*url.URL, 0),
		Downloads: make([]*url.URL, 0),
		Prints:    make([][]string, 0),
	}
}

type DownloadCallback func(PageInfo, io.Reader) error
type PrintCallback func(PageInfo, [][]string)
type Progress func(err error, current uint64, total uint64)

type RecursiveConfig struct {
	Selectors Selectors
	URLs      []string

	Download         func(PageInfo) bool
	DownloadCallback DownloadCallback

	PrintCallback PrintCallback

	ProgressInterval time.Duration
	Progress         Progress

	ReverseLinks     bool
	ReverseDownloads bool
}

func (w *Web) Recurse(ctx context.Context, c RecursiveConfig) error {
	workers := w.c.Concurrency
	if workers <= 0 {
		workers = 8
	}

	urls := make([]*url.URL, len(c.URLs))
	for i, uri := range c.URLs {
		u, err := url.Parse(uri)
		if err != nil {
			return err
		}
		urls[i] = u
	}

	ch := make(chan pageTask, workers)
	chDone := make(chan struct{}, workers)
	done := make(map[string]struct{}, 512)

	tasks := uint64(len(urls))
	var dlCount uint64
	dlTotal := uint64(len(urls))
	var sem sync.RWMutex

	var gerr error

	doDone := func() { atomic.AddUint64(&tasks, ^uint64(0)) }

	var lastProgress time.Time
	progress := func(err error, force bool) {
		if errors.As(err, &context.Canceled) {
			return
		}
		if force || err != nil || time.Since(lastProgress) > c.ProgressInterval {
			c.Progress(err, dlCount, dlTotal)
			lastProgress = time.Now()
		}
	}
	defer progress(nil, true)

	for i := 0; i < workers; i++ {
		go func() {
			for u := range ch {
				if gerr != nil {
					break
				}
				if err := ctx.Err(); err != nil {
					gerr = err
					break
				}

				atomic.AddUint64(&dlCount, 1)
				url := u.URL.String()
				sem.RLock()
				if _, ok := done[url]; ok {
					sem.RUnlock()
					doDone()
					continue
				}
				sem.RUnlock()
				sem.Lock()
				if _, ok := done[url]; ok {
					sem.Unlock()
					doDone()
					continue
				}
				done[url] = struct{}{}
				sem.Unlock()

				if u.Download {
					if c.Download == nil ||
						c.DownloadCallback == nil ||
						!c.Download(u.PageInfo) {
						doDone()
						continue
					}

					err := w.download(ctx, u.PageInfo, c.DownloadCallback)
					progress(err, false)
					doDone()
					continue
				}

				p, err := w.page(ctx, u.PageInfo, c.Selectors)
				if err != nil {
					progress(err, false)
					doDone()
					continue
				}
				atomic.AddUint64(&tasks, uint64(len(p.Links)+len(p.Downloads)))
				atomic.AddUint64(&dlTotal, uint64(len(p.Links)+len(p.Downloads)))

				if c.PrintCallback != nil && len(p.Prints) != 0 {
					c.PrintCallback(p.PageInfo, p.Prints)
				}

				go func() {
					for i, su := range p.Downloads {
						ix := i
						if c.ReverseDownloads {
							ix = len(p.Downloads) - i - 1
						}
						ch <- pageTask{
							PageInfo: PageInfo{
								URL:   su,
								Ref:   &p.PageInfo,
								Title: u.Title,
								Index: ix,
							},
							Download: true,
						}
					}
					for i, su := range p.Links {
						ix := i
						if c.ReverseLinks {
							ix = len(p.Links) - i - 1
						}
						ch <- pageTask{
							PageInfo: PageInfo{
								URL:   su,
								Ref:   &p.PageInfo,
								Index: ix,
							},
						}
					}
				}()

				progress(err, false)
				doDone()
			}
			chDone <- struct{}{}
		}()
	}

	for _, u := range urls {
		ch <- pageTask{PageInfo: PageInfo{URL: u}}
	}

	cnt := workers
	for {
		select {
		case <-chDone:
			cnt--
			if cnt == 0 {
				return gerr
			}
		case <-time.After(time.Millisecond * 300):
			if tasks == 0 {
				return gerr
			}
		}
	}
}
