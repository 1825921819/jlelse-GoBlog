package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/carlmjohnson/requests"
	"github.com/kaorimatz/go-opml"
	"github.com/samber/lo"
	"go.goblog.app/app/pkgs/bufferpool"
	"go.goblog.app/app/pkgs/contenttype"
)

const defaultBlogrollPath = "/blogroll"

func (a *goBlog) serveBlogroll(w http.ResponseWriter, r *http.Request) {
	blog, bc := a.getBlog(r)
	outlines, err, _ := a.blogrollCacheGroup.Do(blog, func() (any, error) {
		return a.getBlogrollOutlines(blog)
	})
	if err != nil {
		log.Printf("Failed to get outlines: %v", err)
		a.serveError(w, r, "", http.StatusInternalServerError)
		return
	}
	c := bc.Blogroll
	can := bc.getRelativePath(defaultIfEmpty(c.Path, defaultBlogrollPath))
	a.render(w, r, a.renderBlogroll, &renderData{
		Canonical: a.getFullAddress(can),
		Data: &blogrollRenderData{
			title:       c.Title,
			description: c.Description,
			outlines:    outlines.([]*opml.Outline),
			download:    can + ".opml",
		},
	})
}

func (a *goBlog) serveBlogrollExport(w http.ResponseWriter, r *http.Request) {
	blog, _ := a.getBlog(r)
	outlines, err, _ := a.blogrollCacheGroup.Do(blog, func() (any, error) {
		return a.getBlogrollOutlines(blog)
	})
	if err != nil {
		log.Printf("Failed to get outlines: %v", err)
		a.serveError(w, r, "", http.StatusInternalServerError)
		return
	}
	opmlBuf := bufferpool.Get()
	defer bufferpool.Put(opmlBuf)
	if err = opml.Render(opmlBuf, &opml.OPML{
		Version:     "2.0",
		DateCreated: time.Now().UTC(),
		Outlines:    outlines.([]*opml.Outline),
	}); err != nil {
		log.Printf("Failed to render OPML: %v", err)
		a.serveError(w, r, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set(contentType, contenttype.XMLUTF8)
	_ = a.min.Get().Minify(contenttype.XML, w, opmlBuf)
}

func (a *goBlog) getBlogrollOutlines(blog string) ([]*opml.Outline, error) {
	config := a.cfg.Blogs[blog].Blogroll
	if cache := a.db.loadOutlineCache(blog); cache != nil {
		return cache, nil
	}
	rb := requests.URL(config.Opml).Client(a.httpClient).UserAgent(appUserAgent)
	if config.AuthHeader != "" && config.AuthValue != "" {
		rb.Header(config.AuthHeader, config.AuthValue)
	}
	var o *opml.OPML
	err := rb.Handle(func(r *http.Response) (err error) {
		defer r.Body.Close()
		o, err = opml.Parse(r.Body)
		return
	}).Fetch(context.Background())
	if err != nil {
		return nil, err
	}
	outlines := o.Outlines
	if len(config.Categories) > 0 {
		filtered := []*opml.Outline{}
		for _, category := range config.Categories {
			if outline, ok := lo.Find(outlines, func(outline *opml.Outline) bool {
				return outline.Title == category || outline.Text == category
			}); ok && outline != nil {
				outline.Outlines = sortOutlines(outline.Outlines)
				filtered = append(filtered, outline)
			}
		}
		outlines = filtered
	} else {
		outlines = sortOutlines(outlines)
	}
	a.db.cacheOutlines(blog, outlines)
	return outlines, nil
}

func (db *database) cacheOutlines(blog string, outlines []*opml.Outline) {
	opmlBuffer := bufferpool.Get()
	_ = opml.Render(opmlBuffer, &opml.OPML{
		Version:     "2.0",
		DateCreated: time.Now().UTC(),
		Outlines:    outlines,
	})
	_ = db.cachePersistently("blogroll_"+blog, opmlBuffer.Bytes())
	bufferpool.Put(opmlBuffer)
}

func (db *database) loadOutlineCache(blog string) []*opml.Outline {
	data, err := db.retrievePersistentCache("blogroll_" + blog)
	if err != nil || data == nil {
		return nil
	}
	o, err := opml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	if time.Since(o.DateCreated).Minutes() > 60 {
		return nil
	}
	return o.Outlines
}

func sortOutlines(outlines []*opml.Outline) []*opml.Outline {
	sort.Slice(outlines, func(i, j int) bool {
		name1 := outlines[i].Title
		if name1 == "" {
			name1 = outlines[i].Text
		}
		name2 := outlines[j].Title
		if name2 == "" {
			name2 = outlines[j].Text
		}
		return strings.ToLower(name1) < strings.ToLower(name2)
	})
	for _, outline := range outlines {
		outline.Outlines = sortOutlines(outline.Outlines)
	}
	return outlines
}
