package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config holds S3-compatible storage configuration.
type S3Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// S3Backend stores objects in an S3-compatible backend (AWS S3, MinIO, etc).
type S3Backend struct {
	client *minio.Client
	bucket string
}

func NewS3Backend(cfg S3Config) (*S3Backend, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}

	// Ensure bucket exists
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}

	return &S3Backend{client: client, bucket: cfg.Bucket}, nil
}

func (s *S3Backend) Read(path string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(context.Background(), s.bucket, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// Verify the object exists by doing a stat
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, err
	}
	return obj, nil
}

func (s *S3Backend) Write(path string, data []byte) error {
	_, err := s.client.PutObject(context.Background(), s.bucket, path, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *S3Backend) Has(path string) (bool, error) {
	_, err := s.client.StatObject(context.Background(), s.bucket, path, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Backend) Delete(path string) error {
	return s.client.RemoveObject(context.Background(), s.bucket, path, minio.RemoveObjectOptions{})
}

func (s *S3Backend) List(prefix string) ([]string, error) {
	var paths []string
	ctx := context.Background()
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if !strings.HasSuffix(obj.Key, "/") {
			paths = append(paths, obj.Key)
		}
	}
	return paths, nil
}

var _ Backend = (*S3Backend)(nil)
