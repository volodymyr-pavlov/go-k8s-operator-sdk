// Command manager runs the operator: it watches the ManagedBucket resources of the cluster and makes the S3 bucket of
// each one match its spec. It serves the health probes and the metrics, and with leader election it lets one replica
// at a time do the work. The flags are described by its -help.
package main
