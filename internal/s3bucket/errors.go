package s3bucket

import "errors"

// ErrNotEmpty is returned by [Store.DeleteBucket] when the bucket still holds objects. S3 does not delete such a
// bucket, and the operator never empties one.
var ErrNotEmpty = errors.New("s3bucket: the bucket is not empty")
