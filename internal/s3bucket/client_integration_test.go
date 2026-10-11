//go:build integration

package s3bucket

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
)

// TestClientAgainstEmulator runs the [Client] against the S3 emulator that AWS_ENDPOINT_URL names.
func TestClientAgainstEmulator(t *testing.T) {
	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("AWS_ENDPOINT_URL is not set")
	}
	ctx := context.Background()
	store := &Client{
		Config: aws.Config{
			Region:       "eu-central-1",
			BaseEndpoint: aws.String(endpoint),
			Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		},
		PathStyle: true,
	}
	want := Bucket{
		Name:       "probe-bucket-0001",
		Region:     "eu-central-1",
		Exists:     true,
		Versioning: storagev1alpha1.VersioningEnabled,
		Tags: map[string]string{
			"team": "platform",
		},
	}

	missing, err := store.ReadBucket(ctx, want.Name, want.Region)
	if err != nil || missing.Exists {
		t.Fatalf("read before create: %+v, %v", missing, err)
	}
	for range 2 {
		if err := store.WriteBucket(ctx, want); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got, err := store.ReadBucket(ctx, want.Name, want.Region)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !got.Exists || got.Versioning != want.Versioning || !sameTags(got.Tags, want.Tags) {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
	want.Versioning = storagev1alpha1.VersioningSuspended
	want.Tags = nil
	if err := store.WriteBucket(ctx, want); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, err = store.ReadBucket(ctx, want.Name, want.Region)
	if err != nil || got.Versioning != want.Versioning || len(got.Tags) != 0 {
		t.Fatalf("read after rewrite: %+v, %v", got, err)
	}
	if err := store.DeleteBucket(ctx, want.Name, want.Region); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteBucket(ctx, want.Name, want.Region); err != nil {
		t.Fatalf("delete twice: %v", err)
	}
	gone, err := store.ReadBucket(ctx, want.Name, want.Region)
	if err != nil || gone.Exists {
		t.Fatalf("read after delete: %+v, %v", gone, err)
	}
}
