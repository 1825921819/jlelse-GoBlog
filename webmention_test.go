package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_webmentions(t *testing.T) {
	app := &goBlog{
		cfg: &config{
			Db: &configDb{
				File: filepath.Join(t.TempDir(), "test.db"),
			},
			Server: &configServer{
				PublicAddress: "https://example.com",
			},
			Blogs: map[string]*configBlog{
				"en": {
					Lang: "en",
				},
			},
			DefaultBlog: "en",
			User:        &configUser{},
		},
	}

	_ = app.initDatabase(false)
	app.initComponents()

	app.db.insertWebmention(&mention{
		Source:  "https://example.net/test",
		Target:  "https://example.com/täst",
		Created: time.Now().Unix(),
		Title:   "Test-Title",
		Content: "Test-Content",
		Author:  "Test-Author",
	}, webmentionStatusVerified)

	mentions, err := app.db.getWebmentions(&webmentionsRequestConfig{
		sourcelike: "example.xyz",
	})
	require.NoError(t, err)
	assert.Len(t, mentions, 0)

	count, err := app.db.countWebmentions(&webmentionsRequestConfig{
		sourcelike: "example.net",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	exists := app.db.webmentionExists("Https://Example.net/test", "Https://Example.com/TÄst")
	assert.True(t, exists)

	mentions = app.db.getWebmentionsByAddress("https://example.com/täst")
	assert.Len(t, mentions, 0)

	mentions, err = app.db.getWebmentions(&webmentionsRequestConfig{
		sourcelike: "example.net",
	})
	require.NoError(t, err)
	if assert.Len(t, mentions, 1) {

		app.db.approveWebmention(mentions[0].ID)

	}

	mentions = app.db.getWebmentionsByAddress("https://example.com/täst")
	assert.Len(t, mentions, 1)

}