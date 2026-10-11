package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Versioning is the versioning state of a bucket, with the names that the S3 API uses.
// +kubebuilder:validation:Enum=Enabled;Suspended
type Versioning string

const (
	// VersioningEnabled keeps every version of an object.
	VersioningEnabled Versioning = "Enabled"

	// VersioningSuspended stops making new versions. It is the state of a bucket that never had versioning.
	VersioningSuspended Versioning = "Suspended"
)

// The condition types of a [ManagedBucket].
const (
	// ConditionReady is true when the bucket exists and matches the spec.
	ConditionReady = "Ready"

	// ConditionDeleting is true while the operator removes the bucket after the resource was deleted.
	ConditionDeleting = "Deleting"
)

// The reasons of the conditions of a [ManagedBucket].
const (
	// ReasonInSync says that the bucket matches the spec.
	ReasonInSync = "InSync"

	// ReasonDriftCorrected says that the bucket had changed outside the cluster and the operator restored it.
	ReasonDriftCorrected = "DriftCorrected"

	// ReasonAWSError says that a call to AWS failed. The operator tries again with a backoff.
	ReasonAWSError = "AWSError"

	// ReasonNotOwned says that a bucket with this name exists but the operator did not create it. The operator does not
	// touch it.
	ReasonNotOwned = "NotOwned"

	// ReasonNotEmpty says that the bucket still holds objects, so it cannot be deleted.
	ReasonNotEmpty = "NotEmpty"
)

// ManagedBucketSpec is the desired state of a [ManagedBucket].
type ManagedBucketSpec struct {
	// BucketName is the name of the S3 bucket. It follows the naming rules of S3 and cannot change.
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bucketName is immutable"
	BucketName string `json:"bucketName"`

	// Region is the AWS region of the bucket. It cannot change.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="region is immutable"
	Region string `json:"region"`

	// Versioning is the versioning state of the bucket.
	// +kubebuilder:default=Suspended
	// +optional
	Versioning Versioning `json:"versioning,omitempty"`

	// Tags are the tags of the bucket. The operator makes the tags of the bucket equal to this map.
	// +kubebuilder:validation:MaxProperties=50
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

// ManagedBucketStatus is the observed state of a [ManagedBucket].
type ManagedBucketStatus struct {
	// ObservedGeneration is the generation of the spec that the conditions describe.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions describe the state of the bucket.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.bucketName`
// +kubebuilder:printcolumn:name="Region",type=string,JSONPath=`.spec.region`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ManagedBucket is an S3 bucket that the operator keeps equal to its spec.
type ManagedBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedBucketSpec   `json:"spec,omitempty"`
	Status ManagedBucketStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedBucketList is a list of [ManagedBucket] resources.
type ManagedBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedBucket `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedBucket{}, &ManagedBucketList{})
}
