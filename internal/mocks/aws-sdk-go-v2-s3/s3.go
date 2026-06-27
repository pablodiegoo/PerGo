package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Options struct {
	BaseEndpoint *string
	UsePathStyle bool
}

type Client struct {
	options Options
}

func NewFromConfig(cfg aws.Config, optFns ...func(*Options)) *Client {
	c := &Client{}
	for _, fn := range optFns {
		fn(&c.options)
	}
	return c
}

type PutObjectInput struct {
	Bucket      *string
	Key         *string
	Body        io.Reader
	ContentType *string
}

type PutObjectOutput struct{}

type GetObjectInput struct {
	Bucket *string
	Key    *string
}

type GetObjectOutput struct {
	Body        io.ReadCloser
	ContentType *string
}

// In-memory mock storage shared by all clients (or client instance, but global is simpler for mock behavior)
var (
	storageMu sync.RWMutex
	// key format: bucket + "/" + key
	mockStorage = make(map[string][]byte)
	mockContentTypes = make(map[string]string)
)

// ResetMockStorage clears storage for tests
func ResetMockStorage() {
	storageMu.Lock()
	defer storageMu.Unlock()
	mockStorage = make(map[string][]byte)
	mockContentTypes = make(map[string]string)
}

func (c *Client) PutObject(ctx context.Context, params *PutObjectInput, optFns ...func(*Options)) (*PutObjectOutput, error) {
	if params.Bucket == nil || params.Key == nil {
		return nil, fmt.Errorf("invalid params")
	}

	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}

	storageMu.Lock()
	defer storageMu.Unlock()

	storageKey := *params.Bucket + "/" + *params.Key
	mockStorage[storageKey] = data
	if params.ContentType != nil {
		mockContentTypes[storageKey] = *params.ContentType
	}

	return &PutObjectOutput{}, nil
}

func (c *Client) GetObject(ctx context.Context, params *GetObjectInput, optFns ...func(*Options)) (*GetObjectOutput, error) {
	if params.Bucket == nil || params.Key == nil {
		return nil, fmt.Errorf("invalid params")
	}

	storageMu.RLock()
	defer storageMu.RUnlock()

	storageKey := *params.Bucket + "/" + *params.Key
	data, exists := mockStorage[storageKey]
	if !exists {
		msg := "The specified key does not exist."
		return nil, &types.NoSuchKey{Message: &msg}
	}

	contentType := mockContentTypes[storageKey]
	return &GetObjectOutput{
		Body:        io.NopCloser(bytes.NewReader(data)),
		ContentType: &contentType,
	}, nil
}
