package store

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

type GCloudStorage struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCloudStorage(ctx context.Context, credentialsFile, bucket, prefix string) (*GCloudStorage, error) {
	var opts []option.ClientOption
	if credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsFile))
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage.NewClient: %w", err)
	}

	return &GCloudStorage{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// ObjectName returns the GCS object name (prefix + "snapshot.pb.gz") of
// the snapshot. Exposed so startup code can log the full path before
// pulling, which makes it easy to confirm both sides are reading from
// the same location.
func (g *GCloudStorage) ObjectName() string {
	return g.prefix + "snapshot.pb.gz"
}

// Bucket returns the GCS bucket name. Exposed for logging alongside
// ObjectName.
func (g *GCloudStorage) Bucket() string {
	return g.bucket
}

// GCSPath returns the canonical gs://bucket/object form of the snapshot
// location. Used in startup logs so an operator can confirm at a glance
// which snapshot the server is loading.
func (g *GCloudStorage) GCSPath() string {
	return "gs://" + g.bucket + "/" + g.ObjectName()
}

func (g *GCloudStorage) Pull(ctx context.Context) ([]byte, error) {
	obj := g.client.Bucket(g.bucket).Object(g.ObjectName())

	reader, err := obj.NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("NewReader: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("ReadAll: %w", err)
	}

	return data, nil
}

func (g *GCloudStorage) Push(ctx context.Context, data []byte) error {
	obj := g.client.Bucket(g.bucket).Object(g.ObjectName())

	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/gzip"

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("Write: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("Close: %w", err)
	}

	return nil
}

func (g *GCloudStorage) Close() error {
	return g.client.Close()
}
