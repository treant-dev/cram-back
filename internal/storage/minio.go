package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioStore struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

func NewMinioStore() (*MinioStore, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	bucket := os.Getenv("MINIO_BUCKET")
	publicURL := os.Getenv("MINIO_PUBLIC_URL")

	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" || publicURL == "" {
		return nil, fmt.Errorf("missing MinIO environment variables")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("make bucket: %w", err)
		}
		policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
		if err := client.SetBucketPolicy(ctx, bucket, policy); err != nil {
			return nil, fmt.Errorf("set bucket policy: %w", err)
		}
	}

	return &MinioStore{client: client, bucket: bucket, publicURL: publicURL}, nil
}

// DeleteURL removes the object identified by its public URL. Errors are logged but not fatal.
func (s *MinioStore) DeleteURL(ctx context.Context, url string) error {
	if url == "" {
		return nil
	}
	base := strings.TrimRight(s.publicURL, "/") + "/" + s.bucket + "/"
	if !strings.HasPrefix(url, base) {
		return nil // not our object, skip
	}
	name := strings.TrimPrefix(url, base)
	return s.client.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{})
}

// DeleteURLs removes multiple objects by their public URLs, best-effort.
func (s *MinioStore) DeleteURLs(ctx context.Context, urls []string) {
	for _, u := range urls {
		_ = s.DeleteURL(ctx, u)
	}
}

// Upload stores the reader as an object and returns its public URL.
func (s *MinioStore) Upload(ctx context.Context, r io.Reader, size int64, contentType, ext string) (string, error) {
	name := uuid.New().String() + ext
	_, err := s.client.PutObject(ctx, s.bucket, name, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("upload object: %w", err)
	}
	base := strings.TrimRight(s.publicURL, "/")
	return fmt.Sprintf("%s/%s/%s", base, s.bucket, name), nil
}
