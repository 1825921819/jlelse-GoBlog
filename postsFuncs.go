package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	gogeouri "git.jlel.se/jlelse/go-geouri"
	"github.com/araddon/dateparse"
	"go.goblog.app/app/pkgs/bufferpool"
	"gopkg.in/yaml.v3"
)

func (a *goBlog) fullPostURL(p *post) string {
	return a.getFullAddress(p.Path)
}

func (a *goBlog) shortPostURL(p *post) string {
	s, err := a.db.shortenPath(p.Path)
	if err != nil {
		return ""
	}
	if a.cfg.Server.ShortPublicAddress != "" {
		return a.cfg.Server.ShortPublicAddress + s
	}
	return a.getFullAddress(s)
}

func (p *post) firstParameter(parameter string) (result string) {
	if pp := p.Parameters[parameter]; len(pp) > 0 {
		result = pp[0]
	}
	return
}

func (a *goBlog) postHtml(p *post, absolute bool) (res string) {
	buf := bufferpool.Get()
	a.postHtmlToWriter(buf, p, absolute)
	res = buf.String()
	bufferpool.Put(buf)
	return
}

func (a *goBlog) postHtmlToWriter(w io.Writer, p *post, absolute bool) {
	// Build HTML
	hb := newHtmlBuilder(w)
	// Add audio to the top
	for _, a := range p.Parameters[a.cfg.Micropub.AudioParam] {
		hb.writeElementOpen("audio", "controls", "preload", "none")
		hb.writeElementOpen("source", "src", a)
		hb.writeElementClose("source")
		hb.writeElementClose("audio")
	}
	// Render markdown
	_ = a.renderMarkdownToWriter(w, p.Content, absolute)
	// Add bookmark links to the bottom
	for _, l := range p.Parameters[a.cfg.Micropub.BookmarkParam] {
		hb.writeElementOpen("p")
		hb.writeElementOpen("a", "class", "u-bookmark-of", "href", l, "target", "_blank", "rel", "noopener noreferrer")
		hb.writeEscaped(l)
		hb.writeElementClose("a")
		hb.writeElementClose("p")
	}
}

func (a *goBlog) feedHtml(w io.Writer, p *post) {
	hb := newHtmlBuilder(w)
	// Add TTS audio to the top
	for _, a := range p.Parameters[ttsParameter] {
		hb.writeElementOpen("audio", "controls", "preload", "none")
		hb.writeElementOpen("source", "src", a)
		hb.writeElementClose("source")
		hb.writeElementClose("audio")
	}
	// Add post HTML
	a.postHtmlToWriter(hb, p, true)
	// Add link to interactions and comments
	blogConfig := a.cfg.Blogs[defaultIfEmpty(p.Blog, a.cfg.DefaultBlog)]
	if cc := blogConfig.Comments; cc != nil && cc.Enabled {
		hb.writeElementOpen("p")
		hb.writeElementOpen("a", "href", a.getFullAddress(p.Path)+"#interactions")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(blogConfig.Lang, "interactions"))
		hb.writeElementClose("a")
		hb.writeElementClose("p")
	}
}

const summaryDivider = "<!--more-->"

func (a *goBlog) postSummary(p *post) (summary string) {
	summary = p.firstParameter("summary")
	if summary != "" {
		return
	}
	splitted := strings.Split(p.Content, summaryDivider)
	hasDivider := len(splitted) > 1
	markdown := splitted[0]
	summary = a.renderText(markdown)
	if !hasDivider {
		summary = strings.Split(summary, "\n\n")[0]
	}
	summary = strings.TrimSpace(strings.ReplaceAll(summary, "\n\n", " "))
	return
}

func (a *goBlog) postTranslations(p *post) []*post {
	translationkey := p.firstParameter("translationkey")
	if translationkey == "" {
		return nil
	}
	posts, err := a.getPosts(&postsRequestConfig{
		parameter:      "translationkey",
		parameterValue: translationkey,
	})
	if err != nil || len(posts) == 0 {
		return nil
	}
	translations := []*post{}
	for _, t := range posts {
		if p.Path != t.Path {
			translations = append(translations, t)
		}
	}
	if len(translations) == 0 {
		return nil
	}
	return translations
}

func (p *post) isPublishedSectionPost() bool {
	return p.Published != "" && p.Section != "" && p.Status == statusPublished
}

func (a *goBlog) postToMfItem(p *post) *microformatItem {
	var mfStatus, mfVisibility string
	switch p.Status {
	case statusDraft:
		mfStatus = "draft"
	case statusPublished, statusScheduled, statusUnlisted, statusPrivate:
		mfStatus = "published"
	case statusPublishedDeleted, statusDraftDeleted, statusPrivateDeleted, statusUnlistedDeleted, statusScheduledDeleted:
		mfStatus = "deleted"
	}
	switch p.Status {
	case statusDraft, statusScheduled, statusPublished:
		mfVisibility = "public"
	case statusUnlisted:
		mfVisibility = "unlisted"
	case statusPrivate:
		mfVisibility = "private"
	case statusPublishedDeleted, statusDraftDeleted, statusPrivateDeleted, statusUnlistedDeleted, statusScheduledDeleted:
		mfVisibility = "deleted"
	}
	return &microformatItem{
		Type: []string{"h-entry"},
		Properties: &microformatProperties{
			Name:       p.Parameters["title"],
			Published:  []string{p.Published},
			Updated:    []string{p.Updated},
			PostStatus: []string{mfStatus},
			Visibility: []string{mfVisibility},
			Category:   p.Parameters[a.cfg.Micropub.CategoryParam],
			Content:    []string{p.contentWithParams()},
			URL:        []string{a.fullPostURL(p)},
			InReplyTo:  p.Parameters[a.cfg.Micropub.ReplyParam],
			LikeOf:     p.Parameters[a.cfg.Micropub.LikeParam],
			BookmarkOf: p.Parameters[a.cfg.Micropub.BookmarkParam],
			MpSlug:     []string{p.Slug},
			Audio:      p.Parameters[a.cfg.Micropub.AudioParam],
			// TODO: Photos
		},
	}
}

func (a *goBlog) showFull(p *post) bool {
	if p.Section == "" {
		return false
	}
	sec, ok := a.cfg.Blogs[p.Blog].Sections[p.Section]
	return ok && sec != nil && sec.ShowFull
}

func (a *goBlog) geoURIs(p *post) []*gogeouri.Geo {
	res := []*gogeouri.Geo{}
	for _, loc := range p.Parameters[a.cfg.Micropub.LocationParam] {
		if loc == "" {
			continue
		}
		g, _ := gogeouri.Parse(loc)
		if g != nil {
			res = append(res, g)
		}
	}
	return res
}

func (a *goBlog) replyLink(p *post) string {
	return p.firstParameter(a.cfg.Micropub.ReplyParam)
}

func (a *goBlog) replyTitle(p *post) string {
	return p.firstParameter(a.cfg.Micropub.ReplyTitleParam)
}

func (a *goBlog) likeLink(p *post) string {
	return p.firstParameter(a.cfg.Micropub.LikeParam)
}

func (a *goBlog) likeTitle(p *post) string {
	return p.firstParameter(a.cfg.Micropub.LikeTitleParam)
}

func (a *goBlog) photoLinks(p *post) []string {
	return p.Parameters[a.cfg.Micropub.PhotoParam]
}

func (p *post) contentWithParams() string {
	params := map[string]any{}
	for k, v := range p.Parameters {
		if l := len(v); l == 1 {
			params[k] = v[0]
		} else if l > 1 {
			params[k] = v
		}
	}
	params["path"] = p.Path
	params["section"] = p.Section
	params["blog"] = p.Blog
	params["published"] = p.Published
	params["updated"] = p.Updated
	params["status"] = string(p.Status)
	params["priority"] = p.Priority
	pb, _ := yaml.Marshal(params)
	return fmt.Sprintf("---\n%s---\n%s", string(pb), p.Content)
}

// Public because of rendering

func (p *post) Title() string {
	return p.firstParameter("title")
}

func (p *post) Old() bool {
	pub := p.Published
	if pub == "" {
		return false
	}
	pubDate, err := dateparse.ParseLocal(pub)
	if err != nil {
		return false
	}
	return pubDate.AddDate(1, 0, 0).Before(time.Now())
}

func (p *post) TTS() string {
	return p.firstParameter(ttsParameter)
}

func (p *post) Deleted() bool {
	return strings.HasSuffix(string(p.Status), statusDeletedSuffix)
}
