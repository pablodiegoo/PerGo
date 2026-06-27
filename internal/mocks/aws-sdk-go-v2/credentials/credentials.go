package credentials

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type StaticCredentialsProvider struct {
	Value aws.Credentials
}

func (s StaticCredentialsProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return s.Value, nil
}

func NewStaticCredentialsProvider(accessKey, secretKey, sessionToken string) StaticCredentialsProvider {
	return StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			SessionToken:    sessionToken,
		},
	}
}
