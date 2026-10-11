package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// The outcomes that [bucketOutcomes] counts.
const (
	outcomeCreated        = "created"
	outcomeDriftCorrected = "drift_corrected"
	outcomeNotOwned       = "not_owned"
	outcomeNotEmpty       = "not_empty"
	outcomeDeleted        = "deleted"
	outcomeAWSError       = "aws_error"
)

// bucketOutcomes counts what each reconcile did to a bucket, by outcome: created, drift_corrected, not_owned,
// not_empty, deleted, and aws_error.
var bucketOutcomes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "managedbucket",
		Name:      "outcomes_total",
		Help:      "Reconcile outcomes for a bucket, by outcome.",
	},
	[]string{"outcome"},
)

func init() {
	metrics.Registry.MustRegister(bucketOutcomes)
}

// count adds one to the counter of an outcome.
func count(outcome string) {
	bucketOutcomes.WithLabelValues(outcome).Inc()
}
