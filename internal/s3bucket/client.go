package s3bucket

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
)

var _ Store = &Client{}

// defaultRegion is the region in which S3 makes a bucket without a location constraint.
const defaultRegion = "us-east-1"

// A Client is the [Store] that talks to S3 with the AWS SDK. It makes one S3 client for each region and keeps it.
type Client struct {
	// Config is the AWS configuration: credentials, retry rules and, for a local S3, the endpoint.
	Config aws.Config

	// PathStyle makes the client put the bucket name in the path of the URL and not in the host name, which an S3
	// emulator such as LocalStack needs.
	PathStyle bool

	// clients holds the S3 client of each region.
	clients sync.Map
}

// ReadBucket implements [Store].
func (c *Client) ReadBucket(ctx context.Context, name, region string) (Bucket, error) {
	api := c.client(region)
	if _, err := api.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)}); err != nil {
		if isMissing(err) {
			return Bucket{
				Name:   name,
				Region: region,
			}, nil
		}
		return Bucket{}, fmt.Errorf("s3bucket: look for bucket %s: %w", name, err)
	}
	versioning, err := api.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(name)})
	if err != nil {
		return Bucket{}, fmt.Errorf("s3bucket: read the versioning of bucket %s: %w", name, err)
	}
	tags, err := readTags(ctx, api, name)
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{
		Name:       name,
		Region:     region,
		Exists:     true,
		Versioning: versioningOf(versioning.Status),
		Tags:       tags,
	}, nil
}

// WriteBucket implements [Store]. It creates the bucket if it is missing, and then sets the versioning and the tags
// that differ.
func (c *Client) WriteBucket(ctx context.Context, bucket Bucket) error {
	api := c.client(bucket.Region)
	current, err := c.ReadBucket(ctx, bucket.Name, bucket.Region)
	if err != nil {
		return err
	}
	if !current.Exists {
		if err := create(ctx, api, bucket); err != nil {
			return err
		}
		current = Bucket{
			Name:       bucket.Name,
			Region:     bucket.Region,
			Exists:     true,
			Versioning: storagev1alpha1.VersioningSuspended,
		}
	}
	if current.Versioning != bucket.Versioning {
		if err := setVersioning(ctx, api, bucket); err != nil {
			return err
		}
	}
	if !sameTags(current.Tags, bucket.Tags) {
		if err := setTags(ctx, api, bucket); err != nil {
			return err
		}
	}
	return nil
}

// DeleteBucket implements [Store].
func (c *Client) DeleteBucket(ctx context.Context, name, region string) error {
	_, err := c.client(region).DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(name)})
	switch {
	case err == nil || isMissing(err):
		return nil
	case isCode(err, "BucketNotEmpty"):
		return fmt.Errorf("%w: %s", ErrNotEmpty, name)
	}
	return fmt.Errorf("s3bucket: delete bucket %s: %w", name, err)
}

// client returns the S3 client of region, and makes it the first time.
func (c *Client) client(region string) *s3.Client {
	if existing, ok := c.clients.Load(region); ok {
		return existing.(*s3.Client)
	}
	made := s3.NewFromConfig(c.Config, func(options *s3.Options) {
		options.Region = region
		options.UsePathStyle = c.PathStyle
	})
	stored, _ := c.clients.LoadOrStore(region, made)
	return stored.(*s3.Client)
}

// create makes the bucket in its region.
func create(ctx context.Context, api *s3.Client, bucket Bucket) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(bucket.Name)}
	if bucket.Region != defaultRegion {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(bucket.Region),
		}
	}
	if _, err := api.CreateBucket(ctx, input); err != nil {
		return fmt.Errorf("s3bucket: create bucket %s: %w", bucket.Name, err)
	}
	return nil
}

// setVersioning sets the versioning state of the bucket.
func setVersioning(ctx context.Context, api *s3.Client, bucket Bucket) error {
	status := types.BucketVersioningStatusSuspended
	if bucket.Versioning == storagev1alpha1.VersioningEnabled {
		status = types.BucketVersioningStatusEnabled
	}
	_, err := api.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  aws.String(bucket.Name),
		VersioningConfiguration: &types.VersioningConfiguration{Status: status},
	})
	if err != nil {
		return fmt.Errorf("s3bucket: set the versioning of bucket %s: %w", bucket.Name, err)
	}
	return nil
}

// setTags makes the tags of the bucket those of the record. S3 cannot hold an empty set, so an empty one removes the tags.
func setTags(ctx context.Context, api *s3.Client, bucket Bucket) error {
	if len(bucket.Tags) == 0 {
		_, err := api.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{Bucket: aws.String(bucket.Name)})
		if err != nil {
			return fmt.Errorf("s3bucket: remove the tags of bucket %s: %w", bucket.Name, err)
		}
		return nil
	}
	set := make([]types.Tag, 0, len(bucket.Tags))
	for key, value := range bucket.Tags {
		set = append(set, types.Tag{
			Key:   aws.String(key),
			Value: aws.String(value),
		})
	}
	_, err := api.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket:  aws.String(bucket.Name),
		Tagging: &types.Tagging{TagSet: set},
	})
	if err != nil {
		return fmt.Errorf("s3bucket: set the tags of bucket %s: %w", bucket.Name, err)
	}
	return nil
}

// readTags reads the tags of the bucket. A bucket with no tag set has none.
func readTags(ctx context.Context, api *s3.Client, name string) (map[string]string, error) {
	output, err := api.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(name)})
	if err != nil {
		if isCode(err, "NoSuchTagSet") {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("s3bucket: read the tags of bucket %s: %w", name, err)
	}
	tags := make(map[string]string, len(output.TagSet))
	for _, tag := range output.TagSet {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return tags, nil
}

// versioningOf maps the status that S3 reports to a [storagev1alpha1.Versioning]. A bucket that never had versioning has
// no status.
func versioningOf(status types.BucketVersioningStatus) storagev1alpha1.Versioning {
	if status == types.BucketVersioningStatusEnabled {
		return storagev1alpha1.VersioningEnabled
	}
	return storagev1alpha1.VersioningSuspended
}

// sameTags reports whether two tag sets hold the same keys and values.
func sameTags(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}

// isMissing reports whether err says that the bucket does not exist.
func isMissing(err error) bool {
	var notFound *types.NotFound
	var noBucket *types.NoSuchBucket
	return errors.As(err, &notFound) || errors.As(err, &noBucket) || isCode(err, "NotFound") || isCode(err, "NoSuchBucket")
}

// isCode reports whether err is an API error with the given code.
func isCode(err error, code string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == code
}
