package main

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-fed/httpsig"
)

var (
	apPrivateKey    *rsa.PrivateKey
	apPostSigner    httpsig.Signer
	apPostSignMutex *sync.Mutex = &sync.Mutex{}
)

func initActivityPub() error {
	pkfile, err := ioutil.ReadFile(appConfig.ActivityPub.KeyPath)
	if err != nil {
		return err
	}
	privateKeyDecoded, _ := pem.Decode(pkfile)
	if privateKeyDecoded == nil {
		return errors.New("failed to decode private key")
	}
	apPrivateKey, err = x509.ParsePKCS1PrivateKey(privateKeyDecoded.Bytes)
	if err != nil {
		return err
	}
	prefs := []httpsig.Algorithm{httpsig.RSA_SHA256}
	digestAlgorithm := httpsig.DigestSha256
	headersToSign := []string{httpsig.RequestTarget, "date", "host", "digest"}
	apPostSigner, _, err = httpsig.NewSigner(prefs, digestAlgorithm, headersToSign, httpsig.Signature, 0)
	if err != nil {
		return err
	}
	return nil
}

func apHandleWebfinger(w http.ResponseWriter, r *http.Request) {
	re, err := regexp.Compile(`^acct:(.*)@` + regexp.QuoteMeta(appConfig.Server.Domain) + `$`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := re.ReplaceAllString(r.URL.Query().Get("resource"), "$1")
	blog := appConfig.Blogs[name]
	if blog == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.Header().Set(contentType, "application/jrd+json"+charsetUtf8Suffix)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"subject": "acct:" + name + "@" + appConfig.Server.Domain,
		"links": []map[string]string{
			{
				"rel":  "self",
				"type": contentTypeAS,
				"href": blog.apIri(),
			},
		},
	})
}

func apHandleInbox(w http.ResponseWriter, r *http.Request) {
	blogName := chi.URLParam(r, "blog")
	blog := appConfig.Blogs[blogName]
	if blog == nil {
		http.Error(w, "Inbox not found", http.StatusNotFound)
		return
	}
	activity := make(map[string]interface{})
	err := json.NewDecoder(r.Body).Decode(&activity)
	_ = r.Body.Close()
	if err != nil {
		http.Error(w, "Failed to decode body", http.StatusBadRequest)
		return
	}
	switch activity["type"] {
	case "Follow":
		apAccept(blogName, blog, activity)
	case "Undo":
		{
			if object, ok := activity["object"].(map[string]interface{}); ok {
				if objectType, ok := object["type"].(string); ok && objectType == "Follow" {
					if iri, ok := object["actor"].(string); ok && iri == activity["actor"] {
						_ = apRemoveFollower(blogName, iri)
					}
				}
			}
		}
	case "Create":
		{
			if object, ok := activity["object"].(map[string]interface{}); ok {
				inReplyTo, hasReplyToString := object["inReplyTo"].(string)
				id, hadID := object["id"].(string)
				if hasReplyToString && hadID && len(inReplyTo) > 0 && len(id) > 0 && strings.Contains(inReplyTo, blog.apIri()) {
					// It's an ActivityPub reply
					// TODO: Save reply to database
				} else if hadID && len(id) > 0 {
					// May be a mention
					// TODO: Save to database
				}
			}
		}
	case "Delete":
		{
			if object, ok := activity["object"].(string); ok && len(object) > 0 && activity["actor"] == object {
				_ = apRemoveFollower(blogName, object)
			}
		}
	case "Like":
	case "Announce":
		{
			// TODO: Save to database
		}
	}
	// Return 201
	w.WriteHeader(http.StatusCreated)

}

func handleWellKnownHostMeta(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(contentType, "application/xrd+xml"+charsetUtf8Suffix)
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="lrdd" type="application/xrd+xml" template="https://` + r.Host + `/.well-known/webfinger?resource={uri}"/></XRD>`))
}

func apGetRemoteActor(iri string) (*asPerson, error) {
	req, err := http.NewRequest(http.MethodGet, iri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", contentTypeAS)
	req.Header.Add("User-Agent", "GoBlog")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if !apRequestIsSuccess(resp.StatusCode) {
		return nil, err
	}
	actor := &asPerson{}
	err = json.NewDecoder(resp.Body).Decode(actor)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}
	return actor, nil
}

func apGetAllFollowers(blog string) (map[string]string, error) {
	rows, err := appDb.Query("select follower, inbox from activitypub_followers where blog = ?", blog)
	if err != nil {
		return nil, err
	}
	followers := map[string]string{}
	for rows.Next() {
		var follower, inbox string
		err = rows.Scan(&follower, &inbox)
		if err != nil {
			return nil, err
		}
		followers[follower] = inbox
	}
	return nil, nil
}

func apAddFollower(blog, follower, inbox string) error {
	startWritingToDb()
	defer finishWritingToDb()
	_, err := appDb.Exec("insert or replace into activitypub_followers (blog, follower, inbox) values (?, ?, ?)", blog, follower, inbox)
	if err != nil {
		return err
	}
	return nil
}

func apRemoveFollower(blog, follower string) error {
	startWritingToDb()
	defer finishWritingToDb()
	_, err := appDb.Exec("delete from activitypub_followers where blog = ? and follower = ?", blog, follower)
	if err != nil {
		return err
	}
	return nil
}

func apPost(p *post) {
	n := p.toASNote()
	create := make(map[string]interface{})
	create["@context"] = asContext
	create["actor"] = appConfig.Blogs[p.Blog].apIri()
	create["id"] = appConfig.Server.PublicAddress + p.Path
	create["published"] = n.Published
	create["type"] = "Create"
	create["object"] = n
	apSendToAllFollowers(p.Blog, create)
}

func apUpdate(p *post) {
	// TODO
}

func apDelete(p *post) {
	// TODO
}

func apAccept(blogName string, blog *configBlog, follow map[string]interface{}) {
	// it's a follow, write it down
	newFollower := follow["actor"].(string)
	log.Println("New follow request:", newFollower)
	// check we aren't following ourselves
	if newFollower == follow["object"] {
		// actor and object are equal
		return
	}
	follower, err := apGetRemoteActor(newFollower)
	if err != nil {
		// Couldn't retrieve remote actor info
		log.Println("Failed to retrieve remote actor info:", newFollower)
		return
	}
	// Add or update follower
	apAddFollower(blogName, follower.ID, follower.Inbox)
	// remove @context from the inner activity
	delete(follow, "@context")
	accept := make(map[string]interface{})
	accept["@context"] = asContext
	accept["to"] = follow["actor"]
	_, accept["id"] = apNewID(blog)
	accept["actor"] = blog.apIri()
	accept["object"] = follow
	accept["type"] = "Accept"
	err = apSendSigned(blog, accept, follower.Inbox)
	if err != nil {
		log.Printf("Failed to accept: %s\n%s\n", follower.ID, err.Error())
		return
	}
	log.Println("Follower accepted:", follower.ID)
}

func apSendToAllFollowers(blog string, activity interface{}) {
	followers, err := apGetAllFollowers(blog)
	if err != nil {
		log.Println("Failed to retrieve followers:", err.Error())
		return

	}
	apSendTo(appConfig.Blogs[blog], activity, followers)
}

func apSendTo(blog *configBlog, activity interface{}, followers map[string]string) {
	for _, i := range followers {
		go func(inbox string) {
			_ = apSendSigned(blog, activity, inbox)
		}(i)
	}
}

func apSendSigned(blog *configBlog, activity interface{}, to string) error {
	// Marshal to json
	body, err := json.Marshal(activity)
	if err != nil {
		return err
	}
	// Copy body to sign it
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	// Create request context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// Create request
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, to, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	iri, err := url.Parse(to)
	if err != nil {
		return err
	}
	r.Header.Add("Accept-Charset", "utf-8")
	r.Header.Add("Date", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05")+" GMT")
	r.Header.Add("User-Agent", "GoBlog")
	r.Header.Add("Accept", contentTypeASUTF8)
	r.Header.Add(contentType, contentTypeASUTF8)
	r.Header.Add("Host", iri.Host)
	// Sign request
	apPostSignMutex.Lock()
	err = apPostSigner.SignRequest(apPrivateKey, blog.apIri()+"#main-key", r, bodyCopy)
	apPostSignMutex.Unlock()
	if err != nil {
		return err
	}
	// Do request
	resp, err := http.DefaultClient.Do(r)
	if !apRequestIsSuccess(resp.StatusCode) {
		body, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("signed request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return err
}

func apNewID(blog *configBlog) (hash string, url string) {
	return hash, blog.apIri() + generateRandomString(16)
}

func (b *configBlog) apIri() string {
	return appConfig.Server.PublicAddress + b.Path
}

func apRequestIsSuccess(code int) bool {
	return code == http.StatusOK || code == http.StatusCreated || code == http.StatusAccepted || code == http.StatusNoContent
}