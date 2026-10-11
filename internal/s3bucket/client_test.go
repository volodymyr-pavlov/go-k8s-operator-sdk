package s3bucket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// fakeS3 starts an HTTP server that answers every request with the given status and S3 error code, and returns a
// [Client] that talks to it.
func fakeS3(t *testing.T, status int, code string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>` + code + `</Code><Message>m</Message></Error>`))
	}))
	t.Cleanup(server.Close)
	return &Client{
		Config: aws.Config{
			Region:           "eu-central-1",
			BaseEndpoint:     aws.String(server.URL),
			Credentials:      credentials.NewStaticCredentialsProvider("test", "test", ""),
			RetryMaxAttempts: 1,
		},
		PathStyle: true,
	}
}

func TestDeleteBucketNotEmpty(t *testing.T) {
	err := fakeS3(t, http.StatusConflict, "BucketNotEmpty").DeleteBucket(context.Background(), "b", "eu-central-1")
	if !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("got %v, want ErrNotEmpty", err)
	}
}

func TestDeleteBucketAlreadyGone(t *testing.T) {
	err := fakeS3(t, http.StatusNotFound, "NoSuchBucket").DeleteBucket(context.Background(), "b", "eu-central-1")
	if err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestReadBucketMissing(t *testing.T) {
	got, err := fakeS3(t, http.StatusNotFound, "NotFound").ReadBucket(context.Background(), "b", "eu-central-1")
	if err != nil || got.Exists {
		t.Fatalf("got %+v, %v, want a missing bucket and no error", got, err)
	}
}

func TestReadBucketFailureIsAnError(t *testing.T) {
	_, err := fakeS3(t, http.StatusServiceUnavailable, "SlowDown").ReadBucket(context.Background(), "b", "eu-central-1")
	if err == nil {
		t.Fatal("got nil, want an error")
	}
}

func TestSameTags(t *testing.T) {
	for name, test := range map[string]struct {
		a, b map[string]string
		want bool
	}{
		"both empty":      {nil, map[string]string{}, true},
		"equal":           {map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		"other value":     {map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		"other key":       {map[string]string{"a": "1"}, map[string]string{"b": "1"}, false},
		"different count": {map[string]string{"a": "1"}, nil, false},
	} {
		if got := sameTags(test.a, test.b); got != test.want {
			t.Errorf("%s: got %v, want %v", name, got, test.want)
		}
	}
}
