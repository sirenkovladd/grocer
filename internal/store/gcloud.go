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

func (g *GCloudStorage) objectName() string {
	return g.prefix + "snapshot.pb.gz"
}

func (g *GCloudStorage) Pull(ctx context.Context) ([]byte, error) {
	obj := g.client.Bucket(g.bucket).Object(g.objectName())

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
	obj := g.client.Bucket(g.bucket).Object(g.objectName())

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
