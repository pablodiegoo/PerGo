package aws

import (
	"context"
	"time"
)

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Source          string
	CanExpire       bool
	Expires         time.Time
}

type CredentialsProvider interface {
	Retrieve(ctx context.Context) (Credentials, error)
}

type Config struct {
	Region              string
	CredentialsProvider CredentialsProvider
}

func String(v string) *string {
	return &v
}
