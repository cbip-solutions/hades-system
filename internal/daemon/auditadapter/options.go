// SPDX-License-Identifier: MIT
package auditadapter

import "context"

type S3Client interface {
	PutObject(ctx context.Context, bucket, key string, body []byte) error
	GetObject(ctx context.Context, bucket, key string) ([]byte, error)
}

type LitestreamMgr interface {
	Status(ctx context.Context) (state string, lagSec int64, err error)
}

type ColdArchiver interface {
	Archive(ctx context.Context, partitionID string) (url, contentHash string, err error)
}

func WithTessera(t TesseraAdapter) Option { return func(a *Adapter) { a.tessera = t } }

func WithS3(s S3Client) Option { return func(a *Adapter) { a.s3 = s } }

func WithLitestream(l LitestreamMgr) Option { return func(a *Adapter) { a.litestream = l } }

func WithColdArchive(c ColdArchiver) Option { return func(a *Adapter) { a.coldArchive = c } }
