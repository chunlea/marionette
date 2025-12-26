package cas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
)

// mockS3Client implements S3Client for testing.
type mockS3Client struct {
	objects map[string][]byte
	putErr  error
	getErr  error
	delErr  error
	headErr error
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{
		objects: make(map[string][]byte),
	}
}

func (m *mockS3Client) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putErr != nil {
		return nil, m.putErr
	}
	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	m.objects[*params.Key] = data
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	data, ok := m.objects[*params.Key]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
	}, nil
}

func (m *mockS3Client) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.delErr != nil {
		return nil, m.delErr
	}
	delete(m.objects, *params.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3Client) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if m.headErr != nil {
		return nil, m.headErr
	}
	data, ok := m.objects[*params.Key]
	if !ok {
		return nil, &types.NotFound{}
	}
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(data))),
	}, nil
}

func TestNewS3Provider(t *testing.T) {
	mock := newMockS3Client()

	// Valid config
	p, err := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})
	require.NoError(t, err)
	assert.NotNil(t, p)

	// Missing client
	_, err = NewS3Provider(nil, S3ProviderConfig{Bucket: "test-bucket"})
	assert.Error(t, err)

	// Missing bucket
	_, err = NewS3Provider(mock, S3ProviderConfig{})
	assert.Error(t, err)
}

func TestS3Provider_Name(t *testing.T) {
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})
	assert.Equal(t, "s3", p.Name())
}

func TestS3Provider_UploadDownload(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	key := "test/file.txt"
	data := []byte("hello world")

	// Upload
	err := p.Upload(ctx, key, bytes.NewReader(data), storage.UploadOptions{
		ContentType: "text/plain",
	})
	require.NoError(t, err)

	// Download
	reader, size, err := p.Download(ctx, key)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	assert.Equal(t, int64(len(data)), size)

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, downloaded)
}

func TestS3Provider_DownloadNotFound(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	_, _, err := p.Download(ctx, "nonexistent")
	assert.Equal(t, storage.ErrNotFound, err)
}

func TestS3Provider_Delete(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	key := "test/file.txt"
	_ = p.Upload(ctx, key, bytes.NewReader([]byte("data")), storage.UploadOptions{})

	err := p.Delete(ctx, key)
	require.NoError(t, err)

	exists, _ := p.Exists(ctx, key)
	assert.False(t, exists)
}

func TestS3Provider_Exists(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	key := "test/file.txt"

	// Should not exist initially
	exists, err := p.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)

	// Upload
	_ = p.Upload(ctx, key, bytes.NewReader([]byte("data")), storage.UploadOptions{})

	// Should exist now
	exists, err = p.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestS3Provider_WithPrefix(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{
		Bucket: "test-bucket",
		Prefix: "my-prefix",
	})

	key := "test/file.txt"
	_ = p.Upload(ctx, key, bytes.NewReader([]byte("data")), storage.UploadOptions{})

	// Verify the key was prefixed
	_, ok := mock.objects["my-prefix/test/file.txt"]
	assert.True(t, ok)
}

func TestS3Provider_UploadError(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	mock.putErr = errors.New("upload failed")
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	err := p.Upload(ctx, "key", bytes.NewReader([]byte("data")), storage.UploadOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload failed")
}

func TestS3Provider_DownloadError(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	mock.getErr = errors.New("download failed")
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	_, _, err := p.Download(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download failed")
}

func TestS3Provider_DeleteError(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	mock.delErr = errors.New("delete failed")
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	err := p.Delete(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

func TestS3Provider_ExistsError(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	mock.headErr = errors.New("head failed")
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	_, err := p.Exists(ctx, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "head failed")
}

func TestS3Provider_WithMetadata(t *testing.T) {
	ctx := context.Background()
	mock := newMockS3Client()
	p, _ := NewS3Provider(mock, S3ProviderConfig{Bucket: "test-bucket"})

	err := p.Upload(ctx, "key", bytes.NewReader([]byte("data")), storage.UploadOptions{
		ContentType: "application/json",
		Metadata: map[string]string{
			"custom": "value",
		},
	})
	require.NoError(t, err)
}
