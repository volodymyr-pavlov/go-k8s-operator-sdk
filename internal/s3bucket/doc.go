// Package s3bucket is the contract between the operator and S3, and the client that implements it with the AWS SDK.
//
// A [Bucket] is the record that travels through the [Store], the interface that the reconciler holds and that a test
// replaces with a fake.
package s3bucket
