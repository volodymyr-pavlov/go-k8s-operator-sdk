package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/internal/s3bucket"
)

// finalizerName is the finalizer that keeps a resource in the cluster until its bucket is gone.
const finalizerName = "storage.example.com/bucket"

// The tags that mark a bucket as one that the operator made for a resource. A bucket without them is never touched.
const (
	ownerTagKey    = "storage.example.com/managed-by"
	ownerTagValue  = "go-k8s-operator-sdk"
	resourceTagKey = "storage.example.com/resource"
)

// The reasons of the events that the reconciler records for a bucket that it made, corrected or removed.
const (
	eventCreated        = "Created"
	eventDriftCorrected = "DriftCorrected"
	eventDeleted        = "Deleted"
)

// An outcome says how a write ended: the event that the user sees, and the reason and the message of the Ready condition.
type outcome struct {
	event   string
	metric  string
	reason  string
	message string
}

// created is the outcome of a write that made the bucket.
var created = outcome{
	event:   eventCreated,
	metric:  outcomeCreated,
	reason:  storagev1alpha1.ReasonInSync,
	message: "The bucket was created.",
}

// corrected is the outcome of a write that restored a bucket that had changed outside the cluster.
var corrected = outcome{
	event:   eventDriftCorrected,
	metric:  outcomeDriftCorrected,
	reason:  storagev1alpha1.ReasonDriftCorrected,
	message: "The bucket had changed outside the cluster, and was restored.",
}

// notEmptyRetry is how long the reconciler waits before it tries again to delete a bucket that still holds objects.
const notEmptyRetry = 30 * time.Second

// ManagedBucketReconciler makes the S3 bucket of each [storagev1alpha1.ManagedBucket] match its spec.
type ManagedBucketReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Buckets reads, writes and deletes the buckets.
	Buckets s3bucket.Store

	// Recorder records the events that the user sees with kubectl describe.
	Recorder record.EventRecorder

	// DriftEvery is how long after a reconcile the bucket is checked again, to find a change that was made outside the
	// cluster. Zero checks only when the resource changes.
	DriftEvery time.Duration
}

// +kubebuilder:rbac:groups=storage.example.com,resources=managedbuckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.example.com,resources=managedbuckets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.example.com,resources=managedbuckets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile makes the bucket of the resource that the request names match its spec, and removes the bucket when the
// resource is deleted. Doing it twice changes nothing the second time. A resource that no longer exists is not an
// error. A failure of AWS is returned, so that the controller tries again with a growing delay, and is also reported in
// the status of the resource.
func (r *ManagedBucketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var resource storagev1alpha1.ManagedBucket
	if err := r.Get(ctx, req.NamespacedName, &resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !resource.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, &resource)
	}
	if controllerutil.AddFinalizer(&resource, finalizerName) {
		if err := r.Update(ctx, &resource); err != nil {
			return ctrl.Result{}, err
		}
	}
	return r.converge(ctx, &resource)
}

// converge makes the bucket exist and match the spec: it creates it, or corrects what differs, and it leaves a bucket
// that the operator did not make alone.
func (r *ManagedBucketReconciler) converge(ctx context.Context, resource *storagev1alpha1.ManagedBucket) (ctrl.Result, error) {
	current, err := r.Buckets.ReadBucket(ctx, resource.Spec.BucketName, resource.Spec.Region)
	if err != nil {
		return r.failed(ctx, resource, err)
	}
	desired := desiredBucket(resource)
	switch {
	case !current.Exists:
		return r.write(ctx, resource, desired, created)
	case !ownedBy(current, resource):
		return r.notOwned(ctx, resource)
	case differs(current, desired):
		return r.write(ctx, resource, desired, corrected)
	}
	return r.ready(ctx, resource, storagev1alpha1.ReasonInSync, "The bucket matches the spec.")
}

// write makes the bucket match desired, records the event of the outcome, and marks the resource ready.
func (r *ManagedBucketReconciler) write(
	ctx context.Context, resource *storagev1alpha1.ManagedBucket, desired s3bucket.Bucket, done outcome,
) (ctrl.Result, error) {
	if err := r.Buckets.WriteBucket(ctx, desired); err != nil {
		return r.failed(ctx, resource, err)
	}
	r.Recorder.Event(resource, corev1.EventTypeNormal, done.event, done.message)
	count(done.metric)
	return r.ready(ctx, resource, done.reason, done.message)
}

// ready marks the resource ready with the reason, and asks to be called again after DriftEvery.
func (r *ManagedBucketReconciler) ready(
	ctx context.Context, resource *storagev1alpha1.ManagedBucket, reason, message string,
) (ctrl.Result, error) {
	r.setReady(resource, metav1.ConditionTrue, reason, message)
	if err := r.Status().Update(ctx, resource); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.DriftEvery}, nil
}

// notOwned marks the resource not ready because a bucket with its name exists that the operator did not make. The
// operator does not touch it, and looks again after DriftEvery, in case it was removed.
func (r *ManagedBucketReconciler) notOwned(ctx context.Context, resource *storagev1alpha1.ManagedBucket) (ctrl.Result, error) {
	message := fmt.Sprintf("A bucket named %s exists that this operator did not create, so it is left alone.", resource.Spec.BucketName)
	r.Recorder.Event(resource, corev1.EventTypeWarning, storagev1alpha1.ReasonNotOwned, message)
	count(outcomeNotOwned)
	r.setReady(resource, metav1.ConditionFalse, storagev1alpha1.ReasonNotOwned, message)
	if err := r.Status().Update(ctx, resource); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.DriftEvery}, nil
}

// failed reports a failure of AWS in the status of the resource and returns it, so that the controller retries.
func (r *ManagedBucketReconciler) failed(ctx context.Context, resource *storagev1alpha1.ManagedBucket, cause error) (ctrl.Result, error) {
	r.Recorder.Event(resource, corev1.EventTypeWarning, storagev1alpha1.ReasonAWSError, cause.Error())
	count(outcomeAWSError)
	r.setReady(resource, metav1.ConditionFalse, storagev1alpha1.ReasonAWSError, cause.Error())
	if err := r.Status().Update(ctx, resource); err != nil {
		return ctrl.Result{}, errors.Join(cause, err)
	}
	return ctrl.Result{}, cause
}

// finalize removes the bucket of a resource that is being deleted, and then lets the resource go. A bucket that is gone,
// or that the operator did not make, is not removed. A bucket that still holds objects is kept, and the removal is tried
// again later.
func (r *ManagedBucketReconciler) finalize(ctx context.Context, resource *storagev1alpha1.ManagedBucket) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(resource, finalizerName) {
		return ctrl.Result{}, nil
	}
	apimeta.SetStatusCondition(&resource.Status.Conditions, metav1.Condition{
		Type:               storagev1alpha1.ConditionDeleting,
		Status:             metav1.ConditionTrue,
		Reason:             "Deleting",
		Message:            "The bucket is being removed.",
		ObservedGeneration: resource.Generation,
	})
	current, err := r.Buckets.ReadBucket(ctx, resource.Spec.BucketName, resource.Spec.Region)
	if err != nil {
		return r.failed(ctx, resource, err)
	}
	if current.Exists && ownedBy(current, resource) {
		if err := r.removeBucket(ctx, resource); err != nil {
			return r.removalFailed(ctx, resource, err)
		}
	}
	controllerutil.RemoveFinalizer(resource, finalizerName)
	return ctrl.Result{}, r.Update(ctx, resource)
}

// removeBucket deletes the bucket of the resource and records that it did.
func (r *ManagedBucketReconciler) removeBucket(ctx context.Context, resource *storagev1alpha1.ManagedBucket) error {
	if err := r.Buckets.DeleteBucket(ctx, resource.Spec.BucketName, resource.Spec.Region); err != nil {
		return err
	}
	r.Recorder.Event(resource, corev1.EventTypeNormal, eventDeleted, "The bucket was removed.")
	count(outcomeDeleted)
	return nil
}

// removalFailed reports why the bucket could not be removed. A bucket that holds objects is kept and the removal is
// tried again after notEmptyRetry. Any other error is a failure of AWS.
func (r *ManagedBucketReconciler) removalFailed(
	ctx context.Context, resource *storagev1alpha1.ManagedBucket, cause error,
) (ctrl.Result, error) {
	if !errors.Is(cause, s3bucket.ErrNotEmpty) {
		return r.failed(ctx, resource, cause)
	}
	r.Recorder.Event(resource, corev1.EventTypeWarning, storagev1alpha1.ReasonNotEmpty, cause.Error())
	count(outcomeNotEmpty)
	r.setReady(resource, metav1.ConditionFalse, storagev1alpha1.ReasonNotEmpty, cause.Error())
	if err := r.Status().Update(ctx, resource); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: notEmptyRetry}, nil
}

// setReady sets the Ready condition and the generation that the conditions describe.
func (r *ManagedBucketReconciler) setReady(resource *storagev1alpha1.ManagedBucket, status metav1.ConditionStatus, reason, message string) {
	resource.Status.ObservedGeneration = resource.Generation
	apimeta.SetStatusCondition(&resource.Status.Conditions, metav1.Condition{
		Type:               storagev1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: resource.Generation,
	})
}

// desiredBucket returns the bucket that the spec describes, with the tags that mark it as owned by the resource.
func desiredBucket(resource *storagev1alpha1.ManagedBucket) s3bucket.Bucket {
	tags := maps.Clone(resource.Spec.Tags)
	if tags == nil {
		tags = make(map[string]string, 2)
	}
	tags[ownerTagKey] = ownerTagValue
	tags[resourceTagKey] = resource.Namespace + "/" + resource.Name
	versioning := resource.Spec.Versioning
	if versioning == "" {
		versioning = storagev1alpha1.VersioningSuspended
	}
	return s3bucket.Bucket{
		Name:       resource.Spec.BucketName,
		Region:     resource.Spec.Region,
		Exists:     true,
		Versioning: versioning,
		Tags:       tags,
	}
}

// ownedBy reports whether the bucket carries the tags that the operator sets on the bucket of this resource.
func ownedBy(bucket s3bucket.Bucket, resource *storagev1alpha1.ManagedBucket) bool {
	return bucket.Tags[ownerTagKey] == ownerTagValue && bucket.Tags[resourceTagKey] == resource.Namespace+"/"+resource.Name
}

// differs reports whether the versioning or the tags of the bucket are not those of the desired one.
func differs(current, desired s3bucket.Bucket) bool {
	return current.Versioning != desired.Versioning || !maps.Equal(current.Tags, desired.Tags)
}

// SetupWithManager registers the reconciler with the manager.
func (r *ManagedBucketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.ManagedBucket{}).
		Named("managedbucket").
		Complete(r)
}
