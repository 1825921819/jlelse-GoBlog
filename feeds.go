package main

import (
	"net/http"
	"strings"
	"time"

	"git.jlel.se/jlelse/GoBlog/pkgs/contenttype"
	"github.com/araddon/dateparse"
	"github.com/gorilla/feeds"
)

type feedType string

const (
	noFeed   feedType = ""
	rssFeed  feedType = "rss"
	atomFeed feedType = "atom"
	jsonFeed feedType = "json"

	feedAudioURL    = "audio"
	feedAudioType   = "audiomime"
	feedAudioLength = "audiolength"
)

func (a *goBlog) generateFeed(blog string, f feedType, w http.ResponseWriter, r *http.Request, posts []*post, title string, description string) {
	now := time.Now()
	if title == "" {
		title = a.cfg.Blogs[blog].Title
	}
	if description == "" {
		description = a.cfg.Blogs[blog].Description
	}
	feed := &feeds.Feed{
		Title:       title,
		Description: description,
		Link:        &feeds.Link{Href: a.getFullAddress(strings.TrimSuffix(r.URL.Path, "."+string(f)))},
		Created:     now,
		Author: &feeds.Author{
			Name:  a.cfg.User.Name,
			Email: a.cfg.User.Email,
		},
		Image: &feeds.Image{
			Url: a.cfg.User.Picture,
		},
	}
	for _, p := range posts {
		created, _ := dateparse.ParseLocal(p.Published)
		updated, _ := dateparse.ParseLocal(p.Updated)
		var enc *feeds.Enclosure
		if p.firstParameter(feedAudioURL) != "" {
			enc = &feeds.Enclosure{
				Url:    p.firstParameter(feedAudioURL),
				Type:   p.firstParameter(feedAudioType),
				Length: p.firstParameter(feedAudioLength),
			}
		}
		feed.Add(&feeds.Item{
			Title:       p.Title(),
			Link:        &feeds.Link{Href: a.fullPostURL(p)},
			Description: a.postSummary(p),
			Id:          p.Path,
			Content:     string(a.absolutePostHTML(p)),
			Created:     created,
			Updated:     updated,
			Enclosure:   enc,
		})
	}
	var err error
	var feedString, feedMediaType string
	switch f {
	case rssFeed:
		feedMediaType = contenttype.RSS
		feedString, err = feed.ToRss()
	case atomFeed:
		feedMediaType = contenttype.ATOM
		feedString, err = feed.ToAtom()
	case jsonFeed:
		feedMediaType = contenttype.JSONFeed
		feedString, err = feed.ToJSON()
	default:
		return
	}
	if err != nil {
		w.Header().Del(contentType)
		a.serveError(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set(contentType, feedMediaType+contenttype.CharsetUtf8Suffix)
	_, _ = a.min.Write(w, feedMediaType, []byte(feedString))
}
