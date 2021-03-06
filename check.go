package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/gzhttp"
	"github.com/samber/lo"
	"go.goblog.app/app/pkgs/bufferpool"
	"golang.org/x/sync/singleflight"
)

func (a *goBlog) checkAllExternalLinks() {
	// Get all published posts without parameters
	posts, err := a.getPosts(&postsRequestConfig{status: statusPublished, withoutParameters: true})
	if err != nil {
		log.Println(err.Error())
		return
	}
	_ = a.checkLinks(log.Writer(), posts...)
}

func (a *goBlog) checkLinks(w io.Writer, posts ...*post) error {
	// Get all links
	allLinks, err := a.allLinks(posts...)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "Checking", len(allLinks), "links")
	// Cancel context
	var canceled, finished atomic.Value
	canceled.Store(false)
	finished.Store(false)
	cancelContext, cancelFunc := context.WithCancel(context.Background())
	a.shutdown.Add(func() {
		if finished.Load().(bool) {
			return
		}
		canceled.Store(true)
		cancelFunc()
		fmt.Fprintln(w, "Canceled link check")
	})
	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: gzhttp.Transport(&http.Transport{
			// Limits
			DisableKeepAlives: true,
			MaxConnsPerHost:   1,
		}),
	}
	// Process all links
	var wg sync.WaitGroup
	var sm sync.Map
	var sg singleflight.Group
	con := make(chan bool, 5)
	for _, l := range allLinks {
		con <- true // This waits until there's space in the buffered channel
		// Check if check is canceled
		if canceled.Load().(bool) {
			break
		}
		// Increase wait group
		wg.Add(1)
		// Start link check
		go func(link *stringPair) {
			defer func() {
				<-con // Read from channel, to let next routine execute
				wg.Done()
			}()
			// Check if link is internal
			if strings.HasPrefix(link.Second, a.cfg.Server.PublicAddress) {
				return
			}
			// Process link
			r, err, _ := sg.Do(link.Second, func() (any, error) {
				// Check if already cached
				if mr, ok := sm.Load(link.Second); ok {
					return mr, nil
				}
				// Do request
				req, err := http.NewRequestWithContext(cancelContext, http.MethodGet, link.Second, nil)
				if err != nil {
					return nil, err
				}
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; rv:60.0) Gecko/20100101 Firefox/60.0")
				req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
				req.Header.Set("Accept-Language", "en-US,en;q=0.5")
				resp, err := client.Do(req)
				if err != nil {
					return nil, err
				}
				_ = resp.Body.Close()
				// Cache result
				sm.Store(link.Second, resp.StatusCode)
				// Return result
				return resp.StatusCode, nil
			})
			// Check error
			if err != nil {
				if !strings.Contains(err.Error(), "context canceled") {
					fmt.Fprintln(w, "Error:", link.Second, err.Error())
				}
				return
			}
			// Check status code
			if statusCode := r.(int); !successStatus(statusCode) {
				fmt.Fprintln(w, link.Second, "in", link.First, statusCode, http.StatusText(statusCode))
			}
		}(l)
	}
	// Wait for all links to finish
	wg.Wait()
	// Finish
	finished.Store(true)
	return nil
}

func (a *goBlog) allLinks(posts ...*post) (allLinks []*stringPair, err error) {
	for _, p := range posts {
		contentBuf := bufferpool.Get()
		a.postHtmlToWriter(contentBuf, p, true)
		links, err := allLinksFromHTML(contentBuf, a.fullPostURL(p))
		bufferpool.Put(contentBuf)
		if err != nil {
			return nil, err
		}
		allLinks = lo.Map(links, func(s string, _ int) *stringPair {
			return &stringPair{a.fullPostURL(p), s}
		})
	}
	return allLinks, nil
}

func successStatus(status int) bool {
	return status >= 200 && status < 400
}
