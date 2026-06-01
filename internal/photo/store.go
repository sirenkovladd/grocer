package photo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"cloud.google.com/go/storage"
)

type Store interface {
	Save(ctx context.Context, receiptID uint64, data []byte) (string, error)
	Get(ctx context.Context, url string) ([]byte, error)
}

type GCloudStore struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCloudStore(client *storage.Client, bucket, prefix string) *GCloudStore {
	return &GCloudStore{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}
}

func (g *GCloudStore) Save(ctx context.Context, receiptID uint64, data []byte) (string, error) {
	objectName := fmt.Sprintf("%s%d.jpg", g.prefix, receiptID)

	obj := g.client.Bucket(g.bucket).Object(objectName)
	writer := obj.NewWriter(ctx)
	writer.ContentType = "image/jpeg"

	if _, err := writer.Write(data); err != nil {
		return "", fmt.Errorf("Write: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("Close: %w", err)
	}

	return fmt.Sprintf("gs://%s/%s", g.bucket, objectName), nil
}

func (g *GCloudStore) Get(ctx context.Context, url string) ([]byte, error) {
	obj := g.client.Bucket(g.bucket).Object(url)

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("NewReader: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("ReadAll: %w", err)
	}

	return data, nil
}

type LocalCache struct {
	dir     string
	maxSize int64
	mu      sync.Mutex
	files   map[string]int64
}

func NewLocalCache(dir string, maxSizeMB int) *LocalCache {
	return &LocalCache{
		dir:     dir,
		maxSize: int64(maxSizeMB) * 1024 * 1024,
		files:   make(map[string]int64),
	}
}

func (c *LocalCache) Get(ctx context.Context, url string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	filename := c.filename(url)
	path := filepath.Join(c.dir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (c *LocalCache) Set(ctx context.Context, url string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	filename := c.filename(url)
	path := filepath.Join(c.dir, filename)

	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return err
	}

	c.evictIfNeeded(int64(len(data)))

	return os.WriteFile(path, data, 0644)
}

func (c *LocalCache) filename(url string) string {
	hash := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x.jpg", hash)
}

func (c *LocalCache) evictIfNeeded(newSize int64) {
	var totalSize int64
	for _, size := range c.files {
		totalSize += size
	}

	if totalSize+newSize > c.maxSize {
		for filename, size := range c.files {
			os.Remove(filepath.Join(c.dir, filename))
			delete(c.files, filename)
			totalSize -= size
			if totalSize+newSize <= c.maxSize {
				break
			}
		}
	}

	c.files[c.filename("")] = newSize
}
