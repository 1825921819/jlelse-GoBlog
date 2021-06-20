package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"git.jlel.se/jlelse/GoBlog/pkgs/contenttype"
)

const micropubMediaSubPath = "/media"

func (a *goBlog) serveMicropubMedia(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Context().Value(indieAuthScope).(string), "media") {
		a.serveError(w, r, "media scope missing", http.StatusForbidden)
		return
	}
	if ct := r.Header.Get(contentType); !strings.Contains(ct, contenttype.MultipartForm) {
		a.serveError(w, r, "wrong content-type", http.StatusBadRequest)
		return
	}
	err := r.ParseMultipartForm(0)
	if err != nil {
		a.serveError(w, r, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		a.serveError(w, r, err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	hashFile, _, _ := r.FormFile("file")
	defer func() { _ = hashFile.Close() }()
	fileName, err := getSHA256(hashFile)
	if err != nil {
		a.serveError(w, r, err.Error(), http.StatusInternalServerError)
		return
	}
	fileExtension := filepath.Ext(header.Filename)
	if len(fileExtension) == 0 {
		// Find correct file extension if original filename does not contain one
		mimeType := header.Header.Get(contentType)
		if len(mimeType) > 0 {
			allExtensions, _ := mime.ExtensionsByType(mimeType)
			if len(allExtensions) > 0 {
				fileExtension = allExtensions[0]
			}
		}
	}
	fileName += strings.ToLower(fileExtension)
	// Save file
	location, err := a.uploadFile(fileName, file)
	if err != nil {
		a.serveError(w, r, "failed to save original file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Try to compress file (only when not in private mode)
	if pm := a.cfg.PrivateMode; pm == nil || !pm.Enabled {
		compressedLocation, compressionErr := a.compressMediaFile(location)
		if compressionErr != nil {
			a.serveError(w, r, "failed to compress file: "+compressionErr.Error(), http.StatusInternalServerError)
			return
		}
		// Overwrite location
		if compressedLocation != "" {
			location = compressedLocation
		}
	}
	http.Redirect(w, r, location, http.StatusCreated)
}

type fileUploadFunc func(filename string, f io.Reader) (location string, err error)

func (a *goBlog) uploadFile(filename string, f io.Reader) (string, error) {
	ms := a.cfg.Micropub.MediaStorage
	if ms != nil && ms.BunnyStorageKey != "" && ms.BunnyStorageName != "" {
		return a.uploadToBunny(filename, f)
	}
	loc, err := saveMediaFile(filename, f)
	if err != nil {
		return "", err
	}
	if ms != nil && ms.MediaURL != "" {
		return ms.MediaURL + loc, nil
	}
	return a.getFullAddress(loc), nil
}

func (a *goBlog) uploadToBunny(filename string, f io.Reader) (location string, err error) {
	config := a.cfg.Micropub.MediaStorage
	if config == nil || config.BunnyStorageName == "" || config.BunnyStorageKey == "" || config.MediaURL == "" {
		return "", errors.New("Bunny storage not completely configured")
	}
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("https://storage.bunnycdn.com/%s/%s", url.PathEscape(config.BunnyStorageName), url.PathEscape(filename)), f)
	req.Header.Add("AccessKey", config.BunnyStorageKey)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", errors.New("failed to upload file to BunnyCDN")
	}
	return config.MediaURL + "/" + filename, nil
}
