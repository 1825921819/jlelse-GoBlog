package main

import (
	"context"
	"database/sql"
	"errors"

	"golang.org/x/crypto/acme/autocert"
)

type httpsCache struct {
	db *database
}

func (c *httpsCache) check() error {
	if c.db == nil {
		return errors.New("no database")
	}
	return nil
}

func (c *httpsCache) Get(_ context.Context, key string) ([]byte, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	d, err := c.db.retrievePersistentCache("https_" + key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, autocert.ErrCacheMiss
	} else if err != nil {
		return nil, err
	}
	return d, nil
}

func (c *httpsCache) Put(_ context.Context, key string, data []byte) error {
	if err := c.check(); err != nil {
		return err
	}
	return c.db.cachePersistently("https_"+key, data)
}

func (c *httpsCache) Delete(_ context.Context, key string) error {
	if err := c.check(); err != nil {
		return err
	}
	return c.db.clearPersistentCache("https_" + key)
}