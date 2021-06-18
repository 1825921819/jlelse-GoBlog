package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_postsDb(t *testing.T) {
	is := assert.New(t)
	must := require.New(t)

	app := &goBlog{
		cfg: &config{
			Blogs: map[string]*configBlog{
				"en": {
					Sections: map[string]*section{
						"test": {},
					},
				},
			},
		},
	}
	app.setInMemoryDatabase()

	now := toLocalSafe(time.Now().String())
	nowPlus1Hour := toLocalSafe(time.Now().Add(1 * time.Hour).String())

	// Save post
	err := app.db.savePost(&post{
		Path:      "/test/abc",
		Content:   "ABC",
		Published: now,
		Updated:   nowPlus1Hour,
		Blog:      "en",
		Section:   "test",
		Status:    statusDraft,
		Parameters: map[string][]string{
			"title": {"Title"},
		},
	}, &postCreationOptions{new: true})
	must.NoError(err)

	// Check post
	p, err := app.db.getPost("/test/abc")
	is.NoError(err)
	is.Equal("/test/abc", p.Path)
	is.Equal("ABC", p.Content)
	is.Equal(now, p.Published)
	is.Equal(nowPlus1Hour, p.Updated)
	is.Equal("en", p.Blog)
	is.Equal("test", p.Section)
	is.Equal(statusDraft, p.Status)
	is.Equal("Title", p.Title())

	// Check number of post paths
	pp, err := app.db.allPostPaths(statusDraft)
	is.NoError(err)
	if is.Len(pp, 1) {
		is.Equal("/test/abc", pp[0])
	}

	pp, err = app.db.allPostPaths(statusPublished)
	is.NoError(err)
	is.Len(pp, 0)

	// Check drafts
	drafts := app.db.getDrafts("en")
	is.Len(drafts, 1)

	// Delete post
	_, err = app.db.deletePost("/test/abc")
	must.NoError(err)

	// Check that there is no post
	count, err := app.db.countPosts(&postsRequestConfig{})
	is.NoError(err)
	is.Equal(0, count)

	// Save published post
	err = app.db.savePost(&post{
		Path:      "/test/abc",
		Content:   "ABC",
		Published: "2021-06-10 10:00:00",
		Updated:   "2021-06-15 10:00:00",
		Blog:      "en",
		Section:   "test",
		Status:    statusPublished,
		Parameters: map[string][]string{
			"tags": {"Test", "Blog"},
		},
	}, &postCreationOptions{new: true})
	must.NoError(err)

	// Check that there is a new post
	count, err = app.db.countPosts(&postsRequestConfig{})
	if is.NoError(err) {
		is.Equal(1, count)
	}

	// Check random post path
	rp, err := app.getRandomPostPath("en")
	if is.NoError(err) {
		is.Equal("/test/abc", rp)
	}

	// Check taxonomies
	tags, err := app.db.allTaxonomyValues("en", "tags")
	if is.NoError(err) {
		is.Len(tags, 2)
		is.Equal([]string{"Test", "Blog"}, tags)
	}

	// Check based on date
	count, err = app.db.countPosts(&postsRequestConfig{
		publishedYear: 2020,
	})
	if is.NoError(err) {
		is.Equal(0, count)
	}

	count, err = app.db.countPosts(&postsRequestConfig{
		publishedYear: 2021,
	})
	if is.NoError(err) {
		is.Equal(1, count)
	}

	// Check dates
	dates, err := app.db.allPublishedDates("en")
	if is.NoError(err) && is.NotEmpty(dates) {
		is.Equal(publishedDate{year: 2021, month: 6, day: 10}, dates[0])
	}

	// Check based on tags
	count, err = app.db.countPosts(&postsRequestConfig{
		parameter:      "tags",
		parameterValue: "ABC",
	})
	if is.NoError(err) {
		is.Equal(0, count)
	}

	count, err = app.db.countPosts(&postsRequestConfig{
		parameter:      "tags",
		parameterValue: "Blog",
	})
	if is.NoError(err) {
		is.Equal(1, count)
	}
}

func Test_ftsWithoutTitle(t *testing.T) {
	// Added because there was a bug where there were no search results without title

	app := &goBlog{}
	app.setInMemoryDatabase()

	err := app.db.savePost(&post{
		Path:      "/test/abc",
		Content:   "ABC",
		Published: toLocalSafe(time.Now().String()),
		Updated:   toLocalSafe(time.Now().Add(1 * time.Hour).String()),
		Blog:      "en",
		Section:   "test",
		Status:    statusDraft,
	}, &postCreationOptions{new: true})
	require.NoError(t, err)

	ps, err := app.db.getPosts(&postsRequestConfig{
		search: "ABC",
	})
	assert.NoError(t, err)
	assert.Len(t, ps, 1)
}
