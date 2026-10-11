# go-k8s-operator-sdk

<div align="center">
<a href="https://gitlab.com/volodymyr-pavlov/go-k8s-operator-sdk/-/pipelines"><img src="https://gitlab.com/volodymyr-pavlov/go-k8s-operator-sdk/badges/main/pipeline.svg" alt="Pipeline status"></a>
<a href="https://gitlab.com/volodymyr-pavlov/go-k8s-operator-sdk/-/releases"><img src="https://gitlab.com/volodymyr-pavlov/go-k8s-operator-sdk/-/badges/release.svg" alt="Latest release"></a>
<a href="go.mod"><img src="https://img.shields.io/badge/go-1.27-00ADD8.svg?logo=go&amp;logoColor=white" alt="Go 1.27"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</div>

A Kubernetes operator in Go that keeps an AWS S3 bucket equal to a custom resource. A user creates a `ManagedBucket` in
the cluster, and the operator makes the real bucket match it. It is built with Operator SDK.

```yaml
apiVersion: storage.example.com/v1alpha1
kind: ManagedBucket
metadata:
  name: managedbucket-sample
spec:
  bucketName: managedbucket-sample-0001
  region: eu-central-1
  versioning: Enabled
  tags:
    team: platform
```

## The resource

| Field               | Meaning                                                                                 |
| ------------------- | --------------------------------------------------------------------------------------- |
| `spec.bucketName`   | The S3 bucket name, 3 to 63 characters. It cannot change.                               |
| `spec.region`       | The AWS region. It cannot change.                                                       |
| `spec.versioning`   | `Enabled` or `Suspended`. The default is `Suspended`.                                   |
| `spec.tags`         | Up to 50 tags. The operator makes the tags of the bucket equal to this map.             |
| `status.conditions` | `Ready` and `Deleting`, with a reason such as `InSync`, `DriftCorrected` or `AWSError`. |

The name and the region are immutable, and a CEL rule on the CRD rejects a change. S3 cannot move a bucket, so a change
would mean a different bucket.

## Run it locally

You need Go 1.27, Docker, `kubectl` and `kind`. No account or paid service is needed: the S3 side is the Floci emulator.

```sh
make local-up      # builds the image, creates a kind cluster, loads the image, deploys the operator and the emulator
kubectl apply -f config/samples/storage_v1alpha1_managedbucket.yaml
kubectl get managedbucket
kubectl describe managedbucket managedbucket-sample   # the conditions and the events
make local-down    # deletes the cluster
```

The operator runs two replicas with leader election, so one of them works at a time. `config/local` adds the emulator
and points the operator at it with a fixed endpoint and dummy credentials, and it must not be used anywhere else.

| Command                 | What it does                                                                           |
| ----------------------- | -------------------------------------------------------------------------------------- |
| `make test`             | Runs the unit and envtest tests against a real API server with a fake S3 store.        |
| `make test-integration` | Starts the Floci emulator in a container and runs the S3 client against it.            |
| `make test-e2e`         | Builds the image, deploys it into a new kind cluster and checks it.                    |
| `make lint staticcheck` | Runs `golangci-lint` and `staticcheck`.                                                |
| `make check-style`      | Checks the style rules of [CONTRIBUTING.md](CONTRIBUTING.md). `make fix-style` fixes.  |
| `make run`              | Runs the operator on this machine, against the current cluster and the AWS credentials. |

## How it works

`Reconcile` is level-triggered and idempotent: it looks at the resource and the bucket, and never at what happened
before.

1. A deleted resource goes to the deletion path, below. Any other resource gets the finalizer `storage.example.com/bucket`.
2. The bucket is read. If it is missing, it is created, with its versioning and tags. If it exists and the operator did
   not make it, nothing changes and the resource is `Ready=False` with the reason `NotOwned`.
3. If its versioning or tags differ from the spec, they are written again, and the reason is `DriftCorrected`. Otherwise
   the reason is `InSync`.
4. After the drift interval (`-drift-interval`, five minutes by default) the resource is reconciled again, so a change
   made in AWS is found without an event in the cluster.

**Ownership.** The operator tags each bucket that it makes with `storage.example.com/managed-by` and
`storage.example.com/resource` (the namespace and name of the resource). It changes or deletes only a bucket that
carries both. A bucket that already exists under the same name is never adopted, because the operator cannot know that
it is safe to change or delete.

**Deletion.** The finalizer keeps the resource until the bucket is gone. A bucket that is already gone, or that the
operator did not make, is skipped, and the resource is released. A bucket that still holds objects is kept: the operator
never empties a bucket, since that destroys data. The resource stays with `Ready=False` and the reason `NotEmpty`, and the
removal is tried again every 30 seconds.

**Failures.** An error of AWS is written to the `Ready` condition and to an event, and it is returned, so controller-runtime
tries again with its exponential backoff (5 ms, doubling, up to about 17 minutes). The AWS SDK also retries a throttled
or failed call by itself, up to three attempts. A step that succeeded is not repeated, because each step compares first.

**Observability.** `managedbucket_outcomes_total` counts what each reconcile did, by outcome: `created`,
`drift_corrected`, `not_owned`, `not_empty`, `deleted` and `aws_error`. The controller-runtime metrics (queue depth,
reconcile time and errors) are served on the same endpoint, over HTTPS with authentication. `/healthz` and `/readyz` are
the probes.

**Permissions.** The Kubernetes RBAC is generated from the markers: the resource, its status and finalizers, and events.
The leader election needs only its lease. On AWS, give the operator the S3 actions `CreateBucket`, `DeleteBucket`,
`HeadBucket`, `GetBucketVersioning`, `PutBucketVersioning`, `GetBucketTagging`, `PutBucketTagging` and
`DeleteBucketTagging`, on the buckets it should manage, for example with IRSA.

## Design decisions and trade-offs

- **A narrow `s3bucket` package.** The reconciler holds three small interfaces (read, write, delete), so the tests use a
  fake of a few lines, and the AWS SDK stays in one file.
- **Tags decide ownership.** It needs no database and survives losing the cluster. The cost: a bucket is made and then
  tagged in separate calls, and a crash between them leaves a bucket without tags that the operator then refuses to
  touch. It is on the list below.
- **Polling for drift.** S3 has no change feed that fits a controller. The interval trades AWS calls (about three per
  resource for each check) against how long a drift lasts. `Reconcile` reads the bucket on every pass, so it is cheap
  when nothing changed.
- **A bucket that holds objects is never emptied.** The user empties it. This avoids data loss and keeps a stuck
  deletion visible.
- **No adoption of existing buckets.** Safer, and less convenient. An `adopt` annotation is the usual answer.
- **One condition per concern.** `Ready` says whether the bucket matches, and `Deleting` says that removal is running.
  The reason names the cause, and the message carries the AWS error text.

## CRD versioning and upgrades

- The group is `storage.example.com` and the version is `v1alpha1`. Alpha means that fields may still change.
- A change that only adds an optional field with a default stays in the same version. A change that renames, removes or
  re-types a field makes a new version, `v1beta1`, then `v1`.
- Both versions are served for a while, with one marked as the storage version. A conversion webhook would convert between
  them, with a test that sends every object through a round trip. The CRD has `status` as a subresource, so the status can be
  written without touching the spec.
- To upgrade: ship the CRD with the new version served and the old one still stored, deploy the operator, run the
  storage-version migration so that the stored objects are rewritten, and only then stop serving the old version. The CRD
  is applied before the operator, and never removed with the operator, since deleting a CRD deletes every resource of it.
- Immutable fields are enforced in the CRD with CEL, so a bad change fails at the API server and not in the operator.

## Disaster recovery

- **The operator is lost or down.** The buckets are untouched and keep working. Resources that are created or changed wait
  for the operator, and resources that are deleted wait for it too, because the finalizer holds them. When it comes back,
  it reconciles every resource once, finds what changed, and makes the buckets match. Nothing is replayed, so nothing is
  done twice. With two replicas, the lease moves to the other one in about 15 seconds.
- **The cluster is lost.** The buckets stay in AWS: deleting the cluster does not delete the resources one by one, so the
  finalizer never runs. In a new cluster, apply the same manifests, kept in Git. A resource with the same namespace and
  name finds its own bucket by the tags and takes it over. A resource under another name sees `NotOwned`, which protects
  the bucket from a mistake, and the fix is to keep the names.
- **A bucket is lost or changed in AWS.** The next check creates it again or restores the versioning and tags. A lost
  bucket's objects come back only from S3 versioning or replication, which are outside this operator.
- **A bad release of the operator.** Roll the Deployment back. The reconcile loop is idempotent, so running the old
  version over what the new one wrote is safe as long as the CRD is compatible, which the versioning rules above keep.
- **Back up** the manifests in Git (GitOps), not the cluster state. The operator keeps no state of its own.

## Organizing a team around several operators

- **One repository for each operator**, from one template: the same layout, the same CI pipeline, the same style checks
  and the same release flow as this one. A new operator starts from it in a day, and an improvement to the template is
  applied to all of them.
- **A small shared Go module** for what every operator repeats: finalizer handling, conditions, outcome metrics, the AWS
  client setup and retry settings. It has a version and a changelog like any other dependency, and it does not hold
  business logic.
- **The CRDs are the API.** They are reviewed like a public API, by the owners of the operator and one reviewer from
  another team, because users depend on them. Breaking changes follow the versioning rules above.
- **Ownership and on-call.** Each operator has an owner team in `CODEOWNERS`, a runbook (what each condition and metric
  means, and how to fix it), dashboards and alerts on the outcome metrics and queue depth, and an error budget.
- **Quality gates.** Every pull request runs the unit, envtest and emulator tests, the linters and the style checks.
  Before a release, an end-to-end run on kind with the emulator. Decisions that are hard to undo go into short
  architecture decision records.
- **Releases** are made from the commit messages with semantic-release, so the version, the changelog and the tag cannot
  differ from what was merged.

## What would come next

- Make the creation of a bucket and its tags atomic from the operator's point of view: write a pending marker to the
  resource status before the creation call, so that a bucket without tags can be recognized as the operator's own.
- An `adopt` annotation, and an optional deletion policy (`Retain` or `Delete`).
- More settings: encryption, public access block and lifecycle rules, with the same drift correction.
- A conversion webhook and a second CRD version, with the storage-version migration job, to prove the upgrade path.
- Tests on a real AWS account, run by hand or on a schedule, against the emulator's differences.
- A Helm chart or an OLM bundle next to the Kustomize bases, and a `PrometheusRule` with the alerts.
- Webhook validation of the tags, and per-resource backoff and rate limits for the AWS calls.

## Third-party libraries

[controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) and [client-go](https://github.com/kubernetes/client-go)
for the controller, [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) for S3,
[prometheus/client_golang](https://github.com/prometheus/client_golang) for the metrics,
[Ginkgo](https://github.com/onsi/ginkgo) and [Gomega](https://github.com/onsi/gomega) for the tests, and envtest for the
API server in the tests. The tools: [Floci](https://floci.io) as the S3 emulator, kind, golangci-lint, staticcheck,
[lefthook](https://github.com/evilmartians/lefthook) and semantic-release. Their versions are in `go.mod`,
`tools/go.mod`, `release/package.json` and the pinned image digests.

## Where things are

```
.
├── api/v1alpha1/          the ManagedBucket types and their markers
├── cmd/                   the program: the manager, its flags, the health probes and the metrics
├── config/                the CRD, RBAC, manager and sample manifests, as Kustomize bases, and config/local
├── internal/controller/   the reconciler, its metrics and its tests
├── internal/s3bucket/     the contract with S3 and the AWS client
├── test/                  the end-to-end tests, which need kind
├── tools/                 golangci-lint, staticcheck, lefthook and the style checks, in a Go module of their own
└── release/               semantic-release and its plugins, pinned for the release job
```

Fixes are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md). MIT licensed. See [LICENSE](LICENSE).
