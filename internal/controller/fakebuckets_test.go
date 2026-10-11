package controller

import (
	"context"
	"maps"
	"sync/atomic"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/internal/s3bucket"
)

var _ s3bucket.Store = &fakeBuckets{}

// fakeBuckets is a [s3bucket.Store] that keeps its buckets in memory, counts the calls that change them, and can be told
// to fail or to hold objects.
type fakeBuckets struct {
	buckets  map[string]s3bucket.Bucket
	writes   atomic.Int32
	deletes  atomic.Int32
	readErr  error
	writeErr error
	notEmpty bool
}

func newFakeBuckets() *fakeBuckets {
	return &fakeBuckets{buckets: map[string]s3bucket.Bucket{}}
}

// ReadBucket implements [s3bucket.Store].
func (f *fakeBuckets) ReadBucket(_ context.Context, name, region string) (s3bucket.Bucket, error) {
	if f.readErr != nil {
		return s3bucket.Bucket{}, f.readErr
	}
	bucket, ok := f.buckets[name]
	if !ok {
		return s3bucket.Bucket{
			Name:   name,
			Region: region,
		}, nil
	}
	bucket.Tags = maps.Clone(bucket.Tags)
	return bucket, nil
}

// WriteBucket implements [s3bucket.Store].
func (f *fakeBuckets) WriteBucket(_ context.Context, bucket s3bucket.Bucket) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes.Add(1)
	bucket.Exists = true
	bucket.Tags = maps.Clone(bucket.Tags)
	f.buckets[bucket.Name] = bucket
	return nil
}

// DeleteBucket implements [s3bucket.Store].
func (f *fakeBuckets) DeleteBucket(_ context.Context, name, _ string) error {
	if f.notEmpty {
		return s3bucket.ErrNotEmpty
	}
	f.deletes.Add(1)
	delete(f.buckets, name)
	return nil
}

// put adds a bucket as if it had been made outside the cluster.
func (f *fakeBuckets) put(name string, tags map[string]string) {
	f.buckets[name] = s3bucket.Bucket{
		Name:       name,
		Region:     "eu-central-1",
		Exists:     true,
		Versioning: storagev1alpha1.VersioningSuspended,
		Tags:       tags,
	}
}
