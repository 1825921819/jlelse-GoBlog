package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_verifyMention(t *testing.T) {

	testHtmlBytes, err := os.ReadFile("testdata/wmtest.html")
	require.NoError(t, err)
	testHtml := string(testHtmlBytes)

	mockClient := &fakeHttpClient{}
	mockClient.setFakeResponse(http.StatusOK, testHtml)

	app := &goBlog{
		httpClient: mockClient,
		cfg: &config{
			Db: &configDb{
				File: filepath.Join(t.TempDir(), "test.db"),
			},
			Server: &configServer{
				PublicAddress: "https://example.org",
			},
		},
		d: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/") {
				http.Redirect(w, r, r.URL.Path[:len(r.URL.Path)-1], http.StatusFound)
			}
		}),
	}

	_ = app.initDatabase(false)
	app.initComponents(false)

	m := &mention{
		Source: "https://example.net/articles/micropub-crossposting-to-twitter-and-enabling-tweetstorms",
		Target: "https://example.org/articles/micropub-syndication-targets-and-crossposting-to-mastodon/",
	}

	err = app.verifyMention(m)
	require.NoError(t, err)

	require.Equal(t, "https://example.org/articles/micropub-syndication-targets-and-crossposting-to-mastodon", m.Target)
	require.Equal(t, "https://example.net/articles/micropub-crossposting-to-twitter-and-enabling-tweetstorms", m.Source)
	require.Equal(t, "https://example.net/articles/micropub-crossposting-to-twitter-and-enabling-tweetstorms", m.Url)
	require.Equal(t, "Micropub, Crossposting to Twitter, and Enabling “Tweetsto…", m.Title)
	require.Equal(t, "I’ve previously talked about how I crosspost from this blog to my Mastodon account without the need for a third-party service, and how I leverage WordPress’s hook system to even enable toot threading. In this post, I’m going to really quickly explain my (extremely similar) Twitter setup. (Note: I don’t actually syndicate this blog’s posts to Twitter, but I do use this very setup on another site of mine.) I liked the idea of a dead-simple Twitter plugin, so I forked my Mastodon plugin and twea…", m.Content)
	require.Equal(t, "Test Blogger", m.Author)

	err = app.verifyMention(m)
	require.NoError(t, err)

}

func Test_verifyMentionBidgy(t *testing.T) {

	testHtmlBytes, err := os.ReadFile("testdata/bridgy.html")
	require.NoError(t, err)
	testHtml := string(testHtmlBytes)

	mockClient := &fakeHttpClient{}
	mockClient.setFakeResponse(http.StatusOK, testHtml)

	app := &goBlog{
		httpClient: mockClient,
		cfg: &config{
			Db: &configDb{
				File: filepath.Join(t.TempDir(), "test.db"),
			},
			Server: &configServer{
				PublicAddress: "https://example.org",
			},
		},
		d: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// do nothing
		}),
	}

	_ = app.initDatabase(false)
	app.initComponents(false)

	m := &mention{
		Source: "https://example.com/abc",
		Target: "https://example.org/walks/2021/11/9k-local-run",
	}

	err = app.verifyMention(m)
	require.NoError(t, err)

	require.Equal(t, "https://example.org/walks/2021/11/9k-local-run", m.Target)
	require.Equal(t, "https://example.com/abc", m.Source)
	require.Equal(t, "https://example.net/notice/ADYb7HhxE6UzPpfFiK", m.Url)
	require.Equal(t, "comment test", m.Title)
	require.Equal(t, "comment test", m.Content)
	require.Equal(t, "m4rk", m.Author)
}

func Test_verifyMentionColin(t *testing.T) {

	testHtmlBytes, err := os.ReadFile("testdata/colin.html")
	require.NoError(t, err)
	testHtml := string(testHtmlBytes)

	mockClient := &fakeHttpClient{}
	mockClient.setFakeResponse(http.StatusOK, testHtml)

	app := &goBlog{
		httpClient: mockClient,
		cfg: &config{
			Db: &configDb{
				File: filepath.Join(t.TempDir(), "test.db"),
			},
			Server: &configServer{
				PublicAddress: "https://jlelse.blog",
			},
		},
		d: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// do nothing
		}),
	}

	_ = app.initDatabase(false)
	app.initComponents(false)

	m := &mention{
		Source: "https://colinwalker.blog/?date=2021-11-14#p3",
		Target: "https://jlelse.blog/micro/2021/11/2021-11-13-lrhvj",
	}

	err = app.verifyMention(m)
	require.NoError(t, err)

	require.Equal(t, "https://jlelse.blog/micro/2021/11/2021-11-13-lrhvj", m.Target)
	require.Equal(t, "https://colinwalker.blog/?date=2021-11-14#p3", m.Source)
	require.Equal(t, "https://colinwalker.blog/?date=2021-11-14#p3", m.Url)
	require.True(t, strings.HasPrefix(m.Title, "Congratulations"))
	require.True(t, strings.HasPrefix(m.Content, "Congratulations"))
	require.Equal(t, "Colin Walker", m.Author)
}
