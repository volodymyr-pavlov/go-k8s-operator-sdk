package s3bucket

import (
	"context"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
)

// A Bucket is an S3 bucket as the operator sees it: what it is called, where it is, and the settings that the operator
// keeps equal to the spec of a resource.
type Bucket struct {
	// Name is the name of the bucket, and Region the AWS region that it lives in.
	Name   string
	Region string

	// Exists is false for a bucket that is not there. The other fields are then empty.
	Exists bool

	// Versioning is the versioning state. A bucket that never had versioning is [storagev1alpha1.VersioningSuspended].
	Versioning storagev1alpha1.Versioning

	// Tags are all the tags of the bucket, including those that the operator sets to mark what it owns.
	Tags map[string]string
}

// A Store reads, writes and deletes buckets.
type Store interface {
	// ReadBucket returns the bucket that the name and the region identify. A bucket that does not exist is a [Bucket]
	// with Exists false, not an error.
	ReadBucket(ctx context.Context, name, region string) (Bucket, error)

	// WriteBucket makes the bucket exist, with the versioning and the tags of bucket, and changes only what differs.
	// Calling it twice with the same record changes nothing the second time.
	WriteBucket(ctx context.Context, bucket Bucket) error

	// DeleteBucket removes the bucket. A bucket that is already gone is not an error. It returns [ErrNotEmpty] if the
	// bucket holds objects.
	DeleteBucket(ctx context.Context, name, region string) error
}
