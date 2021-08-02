package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Login
func (a *goBlog) loginRouter(r chi.Router) {
	r.Use(a.authMiddleware)
	r.Get("/login", serveLogin)
	r.Get("/logout", a.serveLogout)
}

// Micropub
func (a *goBlog) micropubRouter(r chi.Router) {
	r.Use(a.checkIndieAuth)
	r.Get("/", a.serveMicropubQuery)
	r.Post("/", a.serveMicropubPost)
	r.Post(micropubMediaSubPath, a.serveMicropubMedia)
}

// IndieAuth
func (a *goBlog) indieAuthRouter(r chi.Router) {
	r.Get("/", a.indieAuthRequest)
	r.With(a.authMiddleware).Post("/accept", a.indieAuthAccept)
	r.Post("/", a.indieAuthVerification)
	r.Get("/token", a.indieAuthToken)
	r.Post("/token", a.indieAuthToken)
}

// ActivityPub
func (a *goBlog) activityPubRouter(r chi.Router) {
	if a.isPrivate() {
		// Private mode, no ActivityPub
		return
	}
	if ap := a.cfg.ActivityPub; ap != nil && ap.Enabled {
		r.Route("/activitypub", func(r chi.Router) {
			r.Post("/inbox/{blog}", a.apHandleInbox)
			r.Post("/{blog}/inbox", a.apHandleInbox)
		})
		r.Group(func(r chi.Router) {
			r.Use(cacheLoggedIn, a.cacheMiddleware)
			r.Get("/.well-known/webfinger", a.apHandleWebfinger)
			r.Get("/.well-known/host-meta", handleWellKnownHostMeta)
			r.Get("/.well-known/nodeinfo", a.serveNodeInfoDiscover)
			r.Get("/nodeinfo", a.serveNodeInfo)
		})
	}
}

// Webmentions
func (a *goBlog) webmentionsRouter(r chi.Router) {
	if wm := a.cfg.Webmention; wm != nil && wm.DisableReceiving {
		// Disabled
		return
	}
	// Endpoint
	r.Post("/", a.handleWebmention)
	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(a.authMiddleware)
		r.Get("/", a.webmentionAdmin)
		r.Get(paginationPath, a.webmentionAdmin)
		r.Post("/delete", a.webmentionAdminDelete)
		r.Post("/approve", a.webmentionAdminApprove)
		r.Post("/reverify", a.webmentionAdminReverify)
	})
}

// Notifications
func (a *goBlog) notificationsRouter(r chi.Router) {
	r.Use(a.authMiddleware)
	r.Get("/", a.notificationsAdmin)
	r.Get(paginationPath, a.notificationsAdmin)
	r.Post("/delete", a.notificationsAdminDelete)
}

// Assets
func (a *goBlog) assetsRouter(r chi.Router) {
	for _, path := range a.allAssetPaths() {
		r.Get(path, a.serveAsset)
	}
}

// Static files
func (a *goBlog) staticFilesRouter(r chi.Router) {
	r.Use(a.privateModeHandler)
	for _, path := range allStaticPaths() {
		r.Get(path, a.serveStaticFile)
	}
}

// Media files
func (a *goBlog) mediaFilesRouter(r chi.Router) {
	r.Use(a.privateModeHandler)
	r.Get(mediaFileRoute, a.serveMediaFile)
}

// Blog
func (a *goBlog) blogRouter(blog string, conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {

		// Set blog
		r.Use(middleware.WithValue(blogKey, blog))

		// Home
		r.Group(a.blogHomeRouter(conf))

		// Sections
		r.Group(a.blogSectionsRouter(conf))

		// Taxonomies
		r.Group(a.blogTaxonomiesRouter(conf))

		// Dates
		r.Group(a.blogDatesRouter(conf))

		// Photos
		r.Group(a.blogPhotosRouter(conf))

		// Search
		r.Group(a.blogSearchRouter(conf))

		// Custom pages
		r.Group(a.blogCustomPagesRouter(conf))

		// Random post
		r.Group(a.blogRandomRouter(conf))

		// Editor
		r.Route(conf.getRelativePath(editorPath), a.blogEditorRouter(conf))

		// Comments
		r.Group(a.blogCommentsRouter(conf))

		// Stats
		r.Group(a.blogStatsRouter(conf))

		// Blogroll
		r.Group(a.blogBlogrollRouter(conf))

		// Geo map
		r.Group(a.blogGeoMapRouter(conf))

		// Contact
		r.Group(a.blogContactRouter(conf))
	}
}

// Blog - Home
func (a *goBlog) blogHomeRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if !conf.PostAsHome {
			r.Use(a.privateModeHandler)
			r.With(a.checkActivityStreamsRequest, a.cacheMiddleware).Get(conf.getRelativePath(""), a.serveHome)
			r.With(a.cacheMiddleware).Get(conf.getRelativePath("")+feedPath, a.serveHome)
			r.With(a.cacheMiddleware).Get(conf.getRelativePath(paginationPath), a.serveHome)
		}
	}
}

// Blog - Sections
func (a *goBlog) blogSectionsRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(
			a.privateModeHandler,
			a.cacheMiddleware,
		)
		for _, section := range conf.Sections {
			if section.Name != "" {
				r.Group(func(r chi.Router) {
					secPath := conf.getRelativePath(section.Name)
					r.Use(middleware.WithValue(indexConfigKey, &indexConfig{
						path:    secPath,
						section: section,
					}))
					r.Get(secPath, a.serveIndex)
					r.Get(secPath+feedPath, a.serveIndex)
					r.Get(secPath+paginationPath, a.serveIndex)
				})
			}
		}
	}
}

// Blog - Taxonomies
func (a *goBlog) blogTaxonomiesRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(
			a.privateModeHandler,
			a.cacheMiddleware,
		)
		for _, taxonomy := range conf.Taxonomies {
			if taxonomy.Name != "" {
				r.Group(func(r chi.Router) {
					r.Use(middleware.WithValue(taxonomyContextKey, taxonomy))
					taxBasePath := conf.getRelativePath(taxonomy.Name)
					r.Get(taxBasePath, a.serveTaxonomy)
					taxValPath := taxBasePath + "/{taxValue}"
					r.Get(taxValPath, a.serveTaxonomyValue)
					r.Get(taxValPath+feedPath, a.serveTaxonomyValue)
					r.Get(taxValPath+paginationPath, a.serveTaxonomyValue)
				})
			}
		}
	}
}

// Blog - Dates
func (a *goBlog) blogDatesRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(
			a.privateModeHandler,
			a.cacheMiddleware,
		)

		yearPath := conf.getRelativePath(`/{year:x|\d\d\d\d}`)
		r.Get(yearPath, a.serveDate)
		r.Get(yearPath+feedPath, a.serveDate)
		r.Get(yearPath+paginationPath, a.serveDate)

		monthPath := yearPath + `/{month:x|\d\d}`
		r.Get(monthPath, a.serveDate)
		r.Get(monthPath+feedPath, a.serveDate)
		r.Get(monthPath+paginationPath, a.serveDate)

		dayPath := monthPath + `/{day:\d\d}`
		r.Get(dayPath, a.serveDate)
		r.Get(dayPath+feedPath, a.serveDate)
		r.Get(dayPath+paginationPath, a.serveDate)
	}
}

// Blog - Photos
func (a *goBlog) blogPhotosRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if pc := conf.Photos; pc != nil && pc.Enabled {
			photoPath := conf.getRelativePath(defaultIfEmpty(pc.Path, defaultPhotosPath))
			r.Use(
				a.privateModeHandler,
				a.cacheMiddleware,
				middleware.WithValue(indexConfigKey, &indexConfig{
					path:            photoPath,
					parameter:       a.cfg.Micropub.PhotoParam,
					title:           pc.Title,
					description:     pc.Description,
					summaryTemplate: templatePhotosSummary,
				}),
			)
			r.Get(photoPath, a.serveIndex)
			r.Get(photoPath+feedPath, a.serveIndex)
			r.Get(photoPath+paginationPath, a.serveIndex)
		}
	}
}

// Blog - Search
func (a *goBlog) blogSearchRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if bsc := conf.Search; bsc != nil && bsc.Enabled {
			searchPath := conf.getRelativePath(defaultIfEmpty(bsc.Path, defaultSearchPath))
			r.Route(searchPath, func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(
						a.privateModeHandler,
						a.cacheMiddleware,
						middleware.WithValue(pathKey, searchPath),
					)
					r.Get("/", a.serveSearch)
					r.Post("/", a.serveSearch)
					searchResultPath := "/" + searchPlaceholder
					r.Get(searchResultPath, a.serveSearchResult)
					r.Get(searchResultPath+feedPath, a.serveSearchResult)
					r.Get(searchResultPath+paginationPath, a.serveSearchResult)
				})
				r.With(
					// No private mode, to allow using OpenSearch in browser
					a.cacheMiddleware,
					middleware.WithValue(pathKey, searchPath),
				).Get("/opensearch.xml", a.serveOpenSearch)
			})
		}
	}
}

// Blog - Custom pages
func (a *goBlog) blogCustomPagesRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(a.privateModeHandler)
		for _, cp := range conf.CustomPages {
			r.Group(func(r chi.Router) {
				r.Use(middleware.WithValue(customPageContextKey, cp))
				if cp.Cache {
					ce := cp.CacheExpiration
					if ce == 0 {
						ce = a.defaultCacheExpiration()
					}
					r.Use(
						a.cacheMiddleware,
						middleware.WithValue(cacheExpirationKey, ce),
					)
				}
				r.Get(cp.Path, a.serveCustomPage)
			})
		}
	}
}

// Blog - Random
func (a *goBlog) blogRandomRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if rp := conf.RandomPost; rp != nil && rp.Enabled {
			r.With(a.privateModeHandler).Get(conf.getRelativePath(defaultIfEmpty(rp.Path, "/random")), a.redirectToRandomPost)
		}
	}
}

// Blog - Editor
func (a *goBlog) blogEditorRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(a.authMiddleware)
		r.Get("/", a.serveEditor)
		r.Post("/", a.serveEditorPost)
		r.Get("/files", a.serveEditorFiles)
		r.Post("/files/view", a.serveEditorFilesView)
		r.Post("/files/delete", a.serveEditorFilesDelete)
		r.Get("/drafts", a.serveDrafts)
		r.Get("/drafts"+feedPath, a.serveDrafts)
		r.Get("/drafts"+paginationPath, a.serveDrafts)
		r.Get("/private", a.servePrivate)
		r.Get("/private"+feedPath, a.servePrivate)
		r.Get("/private"+paginationPath, a.servePrivate)
		r.Get("/unlisted", a.serveUnlisted)
		r.Get("/unlisted"+feedPath, a.serveUnlisted)
		r.Get("/unlisted"+paginationPath, a.serveUnlisted)
	}
}

// Blog - Comments
func (a *goBlog) blogCommentsRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if commentsConfig := conf.Comments; commentsConfig != nil && commentsConfig.Enabled {
			commentsPath := conf.getRelativePath("/comment")
			r.Route(commentsPath, func(r chi.Router) {
				r.Use(
					a.privateModeHandler,
					middleware.WithValue(pathKey, commentsPath),
				)
				r.With(a.cacheMiddleware, noIndexHeader).Get("/{id:[0-9]+}", a.serveComment)
				r.With(a.captchaMiddleware).Post("/", a.createComment)
				r.Group(func(r chi.Router) {
					// Admin
					r.Use(a.authMiddleware)
					r.Get("/", a.commentsAdmin)
					r.Get(paginationPath, a.commentsAdmin)
					r.Post("/delete", a.commentsAdminDelete)
				})
			})
		}
	}
}

// Blog - Stats
func (a *goBlog) blogStatsRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if bsc := conf.BlogStats; bsc != nil && bsc.Enabled {
			statsPath := conf.getRelativePath(defaultIfEmpty(bsc.Path, defaultBlogStatsPath))
			r.Use(a.privateModeHandler)
			r.With(a.cacheMiddleware).Get(statsPath, a.serveBlogStats)
			r.With(cacheLoggedIn, a.cacheMiddleware).Get(statsPath+blogStatsTablePath, a.serveBlogStatsTable)
		}
	}
}

// Blog - Blogroll
func (a *goBlog) blogBlogrollRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if brConfig := conf.Blogroll; brConfig != nil && brConfig.Enabled {
			brPath := conf.getRelativePath(defaultIfEmpty(brConfig.Path, defaultBlogrollPath))
			r.Use(
				a.privateModeHandler,
				middleware.WithValue(cacheExpirationKey, a.defaultCacheExpiration()),
				a.cacheMiddleware,
			)
			r.Get(brPath, a.serveBlogroll)
			r.Get(brPath+".opml", a.serveBlogrollExport)
		}
	}
}

// Blog - Geo Map
func (a *goBlog) blogGeoMapRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if mc := conf.Map; mc != nil && mc.Enabled {
			mapPath := conf.getRelativePath(defaultIfEmpty(mc.Path, defaultGeoMapPath))
			r.Route(mapPath, func(r chi.Router) {
				r.Use(a.privateModeHandler)
				r.Group(func(r chi.Router) {
					r.With(a.cacheMiddleware).Get("/", a.serveGeoMap)
					r.With(cacheLoggedIn, a.cacheMiddleware).HandleFunc("/leaflet/*", a.serveLeaflet(mapPath+"/"))
				})
				r.Get("/tiles/{z}/{x}/{y}.png", a.proxyTiles(mapPath+"/tiles"))
			})
		}
	}
}

// Blog - Contact
func (a *goBlog) blogContactRouter(conf *configBlog) func(r chi.Router) {
	return func(r chi.Router) {
		if cc := conf.Contact; cc != nil && cc.Enabled {
			contactPath := conf.getRelativePath(defaultIfEmpty(cc.Path, defaultContactPath))
			r.Route(contactPath, func(r chi.Router) {
				r.Use(a.privateModeHandler, a.cacheMiddleware)
				r.Get("/", a.serveContactForm)
				r.With(a.captchaMiddleware).Post("/", a.sendContactSubmission)
			})
		}
	}
}
