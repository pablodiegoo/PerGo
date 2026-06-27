package config

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type LoadOptions func(*aws.Config)

func WithRegion(region string) LoadOptions {
	return func(c *aws.Config) {
		c.Region = region
	}
}

func WithCredentialsProvider(provider aws.CredentialsProvider) LoadOptions {
	return func(c *aws.Config) {
		c.CredentialsProvider = provider
	}
}

func LoadDefaultConfig(ctx context.Context, optFns ...LoadOptions) (aws.Config, error) {
	var c aws.Config
	for _, fn := range optFns {
		fn(&c)
	}
	return c, nil
}
