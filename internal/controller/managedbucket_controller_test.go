package controller

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/prometheus/client_golang/prometheus/testutil"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
)

var _ = Describe("ManagedBucket reconciler", func() {
	var (
		store      *fakeBuckets
		events     *record.FakeRecorder
		reconciler *ManagedBucketReconciler
		resource   *storagev1alpha1.ManagedBucket
		key        types.NamespacedName
	)

	reconcile := func() (ctrl.Result, error) {
		return reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	}
	reload := func() *storagev1alpha1.ManagedBucket {
		var got storagev1alpha1.ManagedBucket
		ExpectWithOffset(1, k8sClient.Get(ctx, key, &got)).To(Succeed())
		return &got
	}
	ready := func() *metav1.Condition {
		return apimeta.FindStatusCondition(reload().Status.Conditions, storagev1alpha1.ConditionReady)
	}

	BeforeEach(func() {
		store = newFakeBuckets()
		events = record.NewFakeRecorder(20)
		reconciler = &ManagedBucketReconciler{
			Client:     k8sClient,
			Scheme:     k8sClient.Scheme(),
			Buckets:    store,
			Recorder:   events,
			DriftEvery: time.Minute,
		}
		key = types.NamespacedName{
			Namespace: "default",
			Name:      "sample",
		}
		resource = &storagev1alpha1.ManagedBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: storagev1alpha1.ManagedBucketSpec{
				BucketName: "sample-bucket-0001",
				Region:     "eu-central-1",
				Versioning: storagev1alpha1.VersioningEnabled,
				Tags: map[string]string{
					"team": "platform",
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		var got storagev1alpha1.ManagedBucket
		if err := k8sClient.Get(ctx, key, &got); err == nil {
			got.Finalizers = nil
			Expect(k8sClient.Update(ctx, &got)).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &got))).To(Succeed())
		}
	})

	It("ignores a resource that is gone", func() {
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "nobody",
		}})
		Expect(err).NotTo(HaveOccurred())
	})

	It("makes the bucket from the spec, marks it as its own, and reports it ready", func() {
		before := testutil.ToFloat64(bucketOutcomes.WithLabelValues(outcomeCreated))
		result, err := reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Minute))
		bucket := store.buckets["sample-bucket-0001"]
		Expect(bucket.Versioning).To(Equal(storagev1alpha1.VersioningEnabled))
		Expect(bucket.Tags).To(HaveKeyWithValue("team", "platform"))
		Expect(bucket.Tags).To(HaveKeyWithValue(ownerTagKey, ownerTagValue))
		Expect(bucket.Tags).To(HaveKeyWithValue(resourceTagKey, "default/sample"))
		got := reload()
		Expect(got.Finalizers).To(ContainElement(finalizerName))
		Expect(ready().Status).To(Equal(metav1.ConditionTrue))
		Expect(ready().Reason).To(Equal(storagev1alpha1.ReasonInSync))
		Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
		Expect(events.Events).To(Receive(ContainSubstring("Created")))
		Expect(testutil.ToFloat64(bucketOutcomes.WithLabelValues(outcomeCreated))).To(Equal(before + 1))
	})

	It("changes nothing when the bucket already matches", func() {
		_, err := reconcile()
		Expect(err).NotTo(HaveOccurred())

		_, err = reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(store.writes.Load()).To(Equal(int32(1)))
		Expect(ready().Reason).To(Equal(storagev1alpha1.ReasonInSync))
	})

	It("corrects a bucket that was changed outside the cluster", func() {
		_, err := reconcile()
		Expect(err).NotTo(HaveOccurred())
		drifted := store.buckets["sample-bucket-0001"]
		drifted.Versioning = storagev1alpha1.VersioningSuspended
		drifted.Tags = map[string]string{
			"team":         "someone-else",
			ownerTagKey:    ownerTagValue,
			resourceTagKey: "default/sample",
		}
		store.buckets["sample-bucket-0001"] = drifted

		_, err = reconcile()

		Expect(err).NotTo(HaveOccurred())
		fixed := store.buckets["sample-bucket-0001"]
		Expect(fixed.Versioning).To(Equal(storagev1alpha1.VersioningEnabled))
		Expect(fixed.Tags).To(HaveKeyWithValue("team", "platform"))
		Expect(ready().Reason).To(Equal(storagev1alpha1.ReasonDriftCorrected))
	})

	It("leaves a bucket that it did not make alone, now and when the resource is deleted", func() {
		store.put("sample-bucket-0001", map[string]string{"owner": "someone"})

		_, err := reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(store.writes.Load()).To(BeZero())
		Expect(ready().Status).To(Equal(metav1.ConditionFalse))
		Expect(ready().Reason).To(Equal(storagev1alpha1.ReasonNotOwned))

		Expect(k8sClient.Delete(ctx, reload())).To(Succeed())
		_, err = reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(store.deletes.Load()).To(BeZero())
		Expect(store.buckets).To(HaveKey("sample-bucket-0001"))
		var got storagev1alpha1.ManagedBucket
		Expect(k8sClient.Get(ctx, key, &got)).NotTo(Succeed())
	})

	It("removes the bucket and then lets the resource go", func() {
		_, err := reconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, reload())).To(Succeed())
		_, err = reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(store.buckets).NotTo(HaveKey("sample-bucket-0001"))
		var got storagev1alpha1.ManagedBucket
		Expect(k8sClient.Get(ctx, key, &got)).NotTo(Succeed())
	})

	It("keeps a bucket that still holds objects, and tries again later", func() {
		_, err := reconcile()
		Expect(err).NotTo(HaveOccurred())
		store.notEmpty = true

		Expect(k8sClient.Delete(ctx, reload())).To(Succeed())
		result, err := reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(notEmptyRetry))
		Expect(store.buckets).To(HaveKey("sample-bucket-0001"))
		Expect(ready().Reason).To(Equal(storagev1alpha1.ReasonNotEmpty))

		store.notEmpty = false
		_, err = reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(store.buckets).NotTo(HaveKey("sample-bucket-0001"))
	})

	It("reports a failure of AWS in the status and returns it, so that the controller retries", func() {
		store.writeErr = errors.New("throttled")

		_, err := reconcile()

		Expect(err).To(MatchError(ContainSubstring("throttled")))
		Expect(ready().Status).To(Equal(metav1.ConditionFalse))
		Expect(ready().Reason).To(Equal(storagev1alpha1.ReasonAWSError))
		Expect(ready().Message).To(ContainSubstring("throttled"))

		store.writeErr = nil
		_, err = reconcile()

		Expect(err).NotTo(HaveOccurred())
		Expect(ready().Status).To(Equal(metav1.ConditionTrue))
	})
})
