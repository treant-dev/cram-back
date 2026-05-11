package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type S3Store struct {
	client    *s3.Client
	bucket    string
	publicURL string
}

func NewS3Store() (*S3Store, error) {
	endpoint  := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	bucket    := os.Getenv("S3_BUCKET")
	publicURL := os.Getenv("S3_PUBLIC_URL")
	region    := os.Getenv("S3_REGION")
	if region == "" {
		region = "us-east-1"
	}

	if accessKey == "" || secretKey == "" || bucket == "" || publicURL == "" {
		return nil, fmt.Errorf("missing S3 environment variables")
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})

	// Ensure bucket exists; create it if not.
	ctx := context.Background()
	if _, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
		if _, err2 := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err2 != nil {
			return nil, fmt.Errorf("create bucket: %w", err2)
		}
		policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
		if _, err2 := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
			Bucket: aws.String(bucket),
			Policy: aws.String(policy),
		}); err2 != nil {
			return nil, fmt.Errorf("set bucket policy: %w", err2)
		}
	}

	return &S3Store{client: client, bucket: bucket, publicURL: publicURL}, nil
}

func (s *S3Store) DeleteURL(ctx context.Context, url string) error {
	if url == "" {
		return nil
	}
	base := strings.TrimRight(s.publicURL, "/") + "/" + s.bucket + "/"
	if !strings.HasPrefix(url, base) {
		return nil
	}
	name := strings.TrimPrefix(url, base)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(name),
	})
	return err
}

func (s *S3Store) DeleteURLs(ctx context.Context, urls []string) {
	for _, u := range urls {
		_ = s.DeleteURL(ctx, u)
	}
}

func (s *S3Store) Upload(ctx context.Context, r io.Reader, size int64, contentType, ext string) (string, error) {
	name := uuid.New().String() + ext
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(name),
		Body:          r,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload object: %w", err)
	}
	base := strings.TrimRight(s.publicURL, "/")
	return fmt.Sprintf("%s/%s/%s", base, s.bucket, name), nil
}
