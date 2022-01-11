package main

import (
	"fmt"
	"html/template"
	"strings"
)

// This file includes some functions that render parts of the HTML

type htmlBuilder struct {
	strings.Builder
}

func (h *htmlBuilder) write(s string) {
	_, _ = h.WriteString(s)
}

func (h *htmlBuilder) writeEscaped(s string) {
	if len(s) == 0 {
		return
	}
	template.HTMLEscape(h, []byte(s))
}

func (h *htmlBuilder) writeAttribute(attr, val string) {
	h.write(` `)
	h.write(attr)
	h.write(`="`)
	h.writeEscaped(val)
	h.write(`"`)
}

func (h *htmlBuilder) writeElementOpen(tag string, attrs ...string) {
	h.write(`<`)
	h.write(tag)
	for i := 0; i < len(attrs); i += 2 {
		h.writeAttribute(attrs[i], attrs[i+1])
	}
	h.write(`>`)
}

func (h *htmlBuilder) writeElementClose(tag string) {
	h.write(`</`)
	h.write(tag)
	h.write(`>`)
}

func (h *htmlBuilder) html() template.HTML {
	return template.HTML(h.String())
}

// Render the HTML for the editor preview
func (a *goBlog) renderEditorPreview(hb *htmlBuilder, bc *configBlog, p *post) {
	if p.RenderedTitle != "" {
		hb.writeElementOpen("h1")
		hb.writeEscaped(p.RenderedTitle)
		hb.writeElementClose("h1")
	}
	a.renderPostMeta(hb, p, bc, "preview")
	if p.Content != "" {
		hb.writeElementOpen("div")
		hb.write(string(a.postHtml(p, true)))
		hb.writeElementClose("div")
	}
	a.renderPostTax(hb, p, bc)
}

type summaryTyp string

const (
	defaultSummary summaryTyp = "summary"
	photoSummary   summaryTyp = "photosummary"
)

// Render the HTML for the post summary on index pages
func (a *goBlog) renderSummary(hb *htmlBuilder, bc *configBlog, p *post, typ summaryTyp) {
	if bc == nil || p == nil {
		return
	}
	if typ == "" {
		typ = defaultSummary
	}
	// Start article
	hb.writeElementOpen("article", "class", "h-entry border-bottom")
	if p.Priority > 0 {
		// Is pinned post
		hb.writeElementOpen("p")
		hb.writeEscaped("📌 ")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(bc.Lang, "pinned"))
		hb.writeElementClose("p")
	}
	if p.RenderedTitle != "" {
		// Has title
		hb.writeElementOpen("h2", "class", "p-name")
		hb.writeElementOpen("a", "class", "u-url", "href", p.Path)
		hb.writeEscaped(p.RenderedTitle)
		hb.writeElementClose("a")
		hb.writeElementClose("h2")
	}
	// Show photos in photo summary
	photos := a.photoLinks(p)
	if typ == photoSummary && len(photos) > 0 {
		for _, photo := range photos {
			hb.write(string(a.safeRenderMarkdownAsHTML(fmt.Sprintf("![](%s)", photo))))
		}
	}
	// Post meta
	a.renderPostMeta(hb, p, bc, "summary")
	if typ != photoSummary && a.showFull(p) {
		// Show full content
		hb.writeElementOpen("div", "class", "e-content")
		hb.write(string(a.postHtml(p, false)))
		hb.writeElementClose("div")
	} else {
		// Show summary
		hb.writeElementOpen("p", "class", "p-summary")
		hb.writeEscaped(a.postSummary(p))
		hb.writeElementClose("p")
	}
	// Show link to full post
	hb.writeElementOpen("p")
	if len(photos) > 0 {
		// Contains photos
		hb.writeEscaped("🖼️ ")
	}
	hb.writeElementOpen("a", "class", "u-url", "href", p.Path)
	hb.writeEscaped(a.ts.GetTemplateStringVariant(bc.Lang, "view"))
	hb.writeElementClose("a")
	hb.writeElementClose("p")
	// Finish article
	hb.writeElementClose("article")
}

// Render the HTML to show the list of post taxonomy values (tags, series, etc.)
func (a *goBlog) renderPostTax(hb *htmlBuilder, p *post, b *configBlog) {
	if b == nil || p == nil {
		return
	}
	// Iterate over all taxonomies
	for _, tax := range b.Taxonomies {
		// Get all sorted taxonomy values for this post
		if taxValues := sortedStrings(p.Parameters[tax.Name]); len(taxValues) > 0 {
			// Start new paragraph
			hb.writeElementOpen("p")
			// Add taxonomy name
			hb.writeElementOpen("strong")
			hb.writeEscaped(a.renderMdTitle(tax.Title))
			hb.writeElementClose("strong")
			hb.write(": ")
			// Add taxonomy values
			for i, taxValue := range taxValues {
				if i > 0 {
					hb.write(", ")
				}
				hb.writeElementOpen(
					"a",
					"class", "p-category",
					"rel", "tag",
					"href", b.getRelativePath(fmt.Sprintf("/%s/%s", tax.Name, urlize(taxValue))),
				)
				hb.writeEscaped(a.renderMdTitle(taxValue))
				hb.writeElementClose("a")
			}
			// End paragraph
			hb.writeElementClose("p")
		}
	}
}

// Render the HTML for the post meta information.
// typ can be "summary", "post" or "preview".
func (a *goBlog) renderPostMeta(hb *htmlBuilder, p *post, b *configBlog, typ string) {
	if b == nil || p == nil || typ != "summary" && typ != "post" && typ != "preview" {
		return
	}
	if typ == "summary" || typ == "post" {
		hb.writeElementOpen("div", "class", "p")
	}
	// Published time
	if published := p.Published; published != "" {
		hb.writeElementOpen("div")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "publishedon"))
		hb.write(" ")
		hb.writeElementOpen("time", "class", "dt-published", "datetime", dateFormat(published, "2006-01-02T15:04:05Z07:00"))
		hb.writeEscaped(isoDateFormat(published))
		hb.writeElementClose("time")
		// Section
		if p.Section != "" {
			if section := b.Sections[p.Section]; section != nil {
				hb.write(" in ") // TODO: Replace with a proper translation
				hb.writeElementOpen("a", "href", b.getRelativePath(section.Name))
				hb.writeEscaped(a.renderMdTitle(section.Title))
				hb.writeElementClose("a")
			}
		}
		hb.writeElementClose("div")
	}
	// Updated time
	if updated := p.Updated; updated != "" {
		hb.writeElementOpen("div")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "updatedon"))
		hb.write(" ")
		hb.writeElementOpen("time", "class", "dt-updated", "datetime", dateFormat(updated, "2006-01-02T15:04:05Z07:00"))
		hb.writeEscaped(isoDateFormat(updated))
		hb.writeElementClose("time")
		hb.writeElementClose("div")
	}
	// IndieWeb Meta
	// Reply ("u-in-reply-to")
	if replyLink := a.replyLink(p); replyLink != "" {
		hb.writeElementOpen("div")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "replyto"))
		hb.writeEscaped(": ")
		hb.writeElementOpen("a", "class", "u-in-reply-to", "rel", "noopener", "target", "_blank", "href", replyLink)
		if replyTitle := a.replyTitle(p); replyTitle != "" {
			hb.writeEscaped(replyTitle)
		} else {
			hb.writeEscaped(replyLink)
		}
		hb.writeElementClose("a")
		hb.writeElementClose("div")
	}
	// Like ("u-like-of")
	if likeLink := a.likeLink(p); likeLink != "" {
		hb.writeElementOpen("div")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "likeof"))
		hb.writeEscaped(": ")
		hb.writeElementOpen("a", "class", "u-like-of", "rel", "noopener", "target", "_blank", "href", likeLink)
		if likeTitle := a.likeTitle(p); likeTitle != "" {
			hb.writeEscaped(likeTitle)
		} else {
			hb.writeEscaped(likeLink)
		}
		hb.writeElementClose("div")
	}
	// Geo
	if geoURI := a.geoURI(p); geoURI != nil {
		hb.writeElementOpen("div")
		hb.writeEscaped("📍 ")
		hb.writeElementOpen("a", "class", "p-location h-geo", "target", "_blank", "rel", "nofollow noopener noreferrer", "href", geoOSMLink(geoURI))
		hb.writeElementOpen("span", "class", "p-name")
		hb.writeEscaped(a.geoTitle(geoURI, b.Lang))
		hb.writeElementClose("span")
		hb.writeElementOpen("data", "class", "p-longitude", "value", fmt.Sprintf("%f", geoURI.Longitude))
		hb.writeElementClose("data")
		hb.writeElementOpen("data", "class", "p-latitude", "value", fmt.Sprintf("%f", geoURI.Latitude))
		hb.writeElementClose("data")
		hb.writeElementClose("a")
		hb.writeElementClose("div")
	}
	// Post specific elements
	if typ == "post" {
		// Translations
		if translations := a.postTranslations(p); len(translations) > 0 {
			hb.writeElementOpen("div")
			hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "translations"))
			hb.writeEscaped(": ")
			for i, translation := range translations {
				if i > 0 {
					hb.writeEscaped(", ")
				}
				hb.writeElementOpen("a", "translate", "no", "href", translation.Path)
				hb.writeEscaped(translation.RenderedTitle)
				hb.writeElementClose("a")
			}
			hb.writeElementClose("div")
		}
		// Short link
		if shortLink := a.shortPostURL(p); shortLink != "" {
			hb.writeElementOpen("div")
			hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "shorturl"))
			hb.writeEscaped(" ")
			hb.writeElementOpen("a", "rel", "shortlink", "href", shortLink)
			hb.writeEscaped(shortLink)
			hb.writeElementClose("a")
			hb.writeElementClose("div")
		}
		// Status
		if p.Status != statusPublished {
			hb.writeElementOpen("div")
			hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "status"))
			hb.writeEscaped(": ")
			hb.writeEscaped(string(p.Status))
			hb.writeElementClose("div")
		}
	}
	if typ == "summary" || typ == "post" {
		hb.writeElementClose("div")
	}
}

// Render the HTML to show a warning for old posts
func (a *goBlog) renderOldContentWarning(hb *htmlBuilder, p *post, b *configBlog) {
	if b == nil || p == nil || !p.Old() {
		return
	}
	hb.writeElementOpen("strong", "class", "p border-top border-bottom")
	hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "oldcontent"))
	hb.writeElementClose("strong")
}

// Render the HTML to show interactions
func (a *goBlog) renderInteractions(hb *htmlBuilder, b *configBlog, canonical string) {
	if b == nil || canonical == "" {
		return
	}
	// Start accordion
	hb.writeElementOpen("details", "class", "p", "id", "interactions")
	hb.writeElementOpen("summary")
	hb.writeElementOpen("strong")
	hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "interactions"))
	hb.writeElementClose("strong")
	hb.writeElementClose("summary")
	// Render mentions
	var renderMentions func(m []*mention)
	renderMentions = func(m []*mention) {
		if len(m) == 0 {
			return
		}
		hb.writeElementOpen("ul")
		for _, mention := range m {
			hb.writeElementOpen("li")
			hb.writeElementOpen("a", "href", mention.Url, "target", "_blank", "rel", "nofollow noopener noreferrer ugc")
			hb.writeEscaped(defaultIfEmpty(mention.Author, mention.Url))
			hb.writeElementClose("a")
			if mention.Title != "" {
				hb.write(" ")
				hb.writeElementOpen("strong")
				hb.writeEscaped(mention.Title)
				hb.writeElementClose("strong")
			}
			if mention.Content != "" {
				hb.write(" ")
				hb.writeElementOpen("i")
				hb.writeEscaped(mention.Content)
				hb.writeElementClose("i")
			}
			if len(mention.Submentions) > 0 {
				renderMentions(mention.Submentions)
			}
			hb.writeElementClose("li")
		}
		hb.writeElementClose("ul")
	}
	renderMentions(a.db.getWebmentionsByAddress(canonical))
	// Show form to send a webmention
	hb.writeElementOpen("form", "class", "fw p", "method", "post", "action", "/webmention")
	hb.writeElementOpen("label", "for", "wm-source", "class", "p")
	hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "interactionslabel"))
	hb.writeElementClose("label")
	hb.writeElementOpen("input", "id", "wm-source", "type", "url", "name", "source", "placeholder", "URL", "required", "")
	hb.writeElementOpen("input", "type", "hidden", "name", "target", "value", canonical)
	hb.writeElementOpen("input", "type", "submit", "value", a.ts.GetTemplateStringVariant(b.Lang, "send"))
	hb.writeElementClose("form")
	// Show form to create a new comment
	hb.writeElementOpen("form", "class", "fw p", "method", "post", "action", "/comment")
	hb.writeElementOpen("input", "type", "hidden", "name", "target", "value", canonical)
	hb.writeElementOpen("input", "type", "text", "name", "name", "placeholder", a.ts.GetTemplateStringVariant(b.Lang, "nameopt"))
	hb.writeElementOpen("input", "type", "url", "name", "website", "placeholder", a.ts.GetTemplateStringVariant(b.Lang, "websiteopt"))
	hb.writeElementOpen("textarea", "name", "comment", "required", "", "placeholder", a.ts.GetTemplateStringVariant(b.Lang, "comment"))
	hb.writeElementClose("textarea")
	hb.writeElementOpen("input", "type", "submit", "value", a.ts.GetTemplateStringVariant(b.Lang, "docomment"))
	hb.writeElementClose("form")
	// Finish accordion
	hb.writeElementClose("details")
}

// Render HTML for author h-card
func (a *goBlog) renderAuthor(hb *htmlBuilder) {
	user := a.cfg.User
	if user == nil {
		return
	}
	hb.writeElementOpen("div", "class", "p-author h-card hide")
	if user.Picture != "" {
		hb.writeElementOpen("data", "class", "u-photo", "value", user.Picture)
		hb.writeElementClose("data")
	}
	if user.Name != "" {
		hb.writeElementOpen("a", "class", "p-name u-url", "rel", "me", "href", defaultIfEmpty(user.Link, "/"))
		hb.writeEscaped(user.Name)
		hb.writeElementClose("a")
	}
	hb.writeElementClose("div")
}

// Render HTML that includes the head meta tags for a post
func (a *goBlog) renderPostHeadMeta(hb *htmlBuilder, p *post, canonical string) {
	if p == nil {
		return
	}
	if canonical != "" {
		hb.writeElementOpen("meta", "property", "og:url", "content", canonical)
		hb.writeElementOpen("meta", "property", "twitter:url", "content", canonical)
	}
	if p.RenderedTitle != "" {
		hb.writeElementOpen("meta", "property", "og:title", "content", p.RenderedTitle)
		hb.writeElementOpen("meta", "property", "twitter:title", "content", p.RenderedTitle)
	}
	if summary := a.postSummary(p); summary != "" {
		hb.writeElementOpen("meta", "name", "description", "content", summary)
		hb.writeElementOpen("meta", "property", "og:description", "content", summary)
		hb.writeElementOpen("meta", "property", "twitter:description", "content", summary)
	}
	if p.Published != "" {
		hb.writeElementOpen("meta", "itemprop", "datePublished", "content", dateFormat(p.Published, "2006-01-02T15:04:05-07:00"))
	}
	if p.Updated != "" {
		hb.writeElementOpen("meta", "itemprop", "dateModified", "content", dateFormat(p.Updated, "2006-01-02T15:04:05-07:00"))
	}
	for _, img := range a.photoLinks(p) {
		hb.writeElementOpen("meta", "itemprop", "image", "content", img)
		hb.writeElementOpen("meta", "property", "og:image", "content", img)
		hb.writeElementOpen("meta", "property", "twitter:image", "content", img)
	}
}

// Render HTML for TOR notice in the footer
func (a *goBlog) renderTorNotice(hb *htmlBuilder, b *configBlog, torUsed bool, torAddress string) {
	if !a.cfg.Server.Tor || b == nil || !torUsed && torAddress == "" {
		return
	}
	if torUsed {
		hb.writeElementOpen("p", "id", "tor")
		hb.writeEscaped("🔐 ")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "connectedviator"))
		hb.writeElementClose("p")
	} else if torAddress != "" {
		hb.writeElementOpen("p", "id", "tor")
		hb.writeEscaped("🔓 ")
		hb.writeElementOpen("a", "href", torAddress)
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "connectviator"))
		hb.writeElementClose("a")
		hb.writeEscaped(" ")
		hb.writeElementOpen("a", "href", "https://www.torproject.org/", "target", "_blank", "rel", "nofollow noopener noreferrer")
		hb.writeEscaped(a.ts.GetTemplateStringVariant(b.Lang, "whatistor"))
		hb.writeElementClose("a")
		hb.writeElementClose("p")
	}
}
