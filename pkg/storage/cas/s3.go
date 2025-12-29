package cas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/chunlea/marionette/pkg/storage"
)

// S3Client defines the S3 operations used by S3Provider.
// This interface allows for mocking in tests.
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// S3Provider stores blobs in Amazon S3 or S3-compatible storage.
type S3Provider struct {
	client S3Client
	bucket string
	prefix string
}

// S3ProviderConfig contains configuration for the S3 provider.
type S3ProviderConfig struct {
	// Bucket is the S3 bucket name.
	Bucket string

	// Prefix is an optional prefix for all keys.
	Prefix string
}

// NewS3Provider creates a new S3 storage provider.
func NewS3Provider(client S3Client, config S3ProviderConfig) (*S3Provider, error) {
	if client == nil {
		return nil, errors.New("S3 client is required")
	}
	if config.Bucket == "" {
		return nil, errors.New("S3 bucket is required")
	}
	return &S3Provider{
		client: client,
		bucket: config.Bucket,
		prefix: config.Prefix,
	}, nil
}

// Name returns the provider name.
func (p *S3Provider) Name() string {
	return "s3"
}

// fullKey returns the full S3 key with prefix.
func (p *S3Provider) fullKey(key string) string {
	if p.prefix == "" {
		return key
	}
	return p.prefix + "/" + key
}

// Upload writes data to the given key.
func (p *S3Provider) Upload(ctx context.Context, key string, r io.Reader, opts storage.UploadOptions) error {
	// Read all data into memory for S3 upload
	// For large files, consider using multipart upload
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(p.fullKey(key)),
		Body:   bytes.NewReader(data),
	}

	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}

	_, err = p.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Download returns a reader for the given key.
// Caller must close the returned reader.
func (p *S3Provider) Download(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(p.fullKey(key)),
	}

	result, err := p.client.GetObject(ctx, input)
	if err != nil {
		var notFound *types.NoSuchKey
		if errors.As(err, &notFound) {
			return nil, 0, storage.ErrNotFound
		}
		// Also check for NotFound error type (some S3-compatible services)
		var notFoundErr *types.NotFound
		if errors.As(err, &notFoundErr) {
			return nil, 0, storage.ErrNotFound
		}
		return nil, 0, fmt.Errorf("failed to download from S3: %w", err)
	}

	size := int64(0)
	if result.ContentLength != nil {
		size = *result.ContentLength
	}

	return result.Body, size, nil
}

// Delete removes the object at the given key.
// Returns nil if the object doesn't exist (idempotent).
func (p *S3Provider) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(p.fullKey(key)),
	}

	_, err := p.client.DeleteObject(ctx, input)
	if err != nil {
		// S3 DeleteObject is idempotent, so we shouldn't get NotFound errors
		// But handle it just in case with some S3-compatible services
		var notFound *types.NoSuchKey
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// Exists checks if the object exists.
func (p *S3Provider) Exists(ctx context.Context, key string) (bool, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(p.fullKey(key)),
	}

	_, err := p.client.HeadObject(ctx, input)
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		// Also check for NoSuchKey
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check S3 object: %w", err)
	}

	return true, nil
}

// Compile-time interface check.
var _ storage.StorageProvider = (*S3Provider)(nil)
