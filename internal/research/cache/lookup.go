//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import (
	"context"
	"errors"
)

func Lookup(ctx context.Context, db *DB, query string, embedding []float32, projectID, sessionID string) (*LookupResult, error) {

	res, err := LookupExact(ctx, db, query, projectID, sessionID)
	if err == nil {

		return res, nil
	}
	if !errors.Is(err, ErrCacheMiss) {

		return nil, err
	}

	if embedding == nil {
		return nil, ErrCacheMiss
	}

	res, err = LookupSemantic(ctx, db, embedding, projectID, sessionID)
	if err == nil {
		return res, nil
	}
	if !errors.Is(err, ErrCacheMiss) {

		return nil, err
	}

	return nil, ErrCacheMiss
}
