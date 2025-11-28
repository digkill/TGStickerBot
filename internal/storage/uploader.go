package storage

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

type Config struct {
	Endpoint      string
	Region        string
	AccessKey     string
	SecretKey     string
	Bucket        string
	PublicBaseURL string
	UsePathStyle  bool
	Prefix        string
}

type Uploader struct {
	cfg    Config
	client *s3.Client
}

func NewUploader(cfg Config) (*Uploader, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("s3 region is required")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3 credentials are required")
	}
	if cfg.PublicBaseURL == "" {
		return nil, fmt.Errorf("s3 public base url is required")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "references"
	}

	options := s3.Options{
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: cfg.UsePathStyle,
	}
	if cfg.Endpoint != "" {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
	}

	client := s3.New(options)

	return &Uploader{
		cfg:    cfg,
		client: client,
	}, nil
}

func (u *Uploader) Upload(ctx context.Context, data []byte, contentType string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("no data to upload")
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}

	key := u.generateKey(contentType)
	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(u.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
		ACL:         types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		return "", fmt.Errorf("upload to s3: %w", err)
	}
	return strings.TrimRight(u.cfg.PublicBaseURL, "/") + "/" + key, nil
}

func (u *Uploader) generateKey(contentType string) string {
	ext := extensionFromContentType(contentType)
	now := time.Now().UTC()
	prefix := strings.Trim(u.cfg.Prefix, "/")
	key := path.Join(prefix, fmt.Sprintf("%04d/%02d/%02d", now.Year(), now.Month(), now.Day()), uuid.NewString()+ext)
	return key
}

func extensionFromContentType(contentType string) string {
	switch strings.ToLower(contentType) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
