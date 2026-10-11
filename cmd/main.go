package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	storagev1alpha1 "github.com/volodymyr-pavlov/go-k8s-operator-sdk/api/v1alpha1"
	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/internal/controller"
	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/internal/s3bucket"
	// +kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()
var setupLog = ctrl.Log.WithName("setup")

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(storagev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// options holds the command-line options of the manager.
type options struct {
	metricsAddr          string
	metricsCertPath      string
	metricsCertName      string
	metricsCertKey       string
	probeAddr            string
	secureMetrics        bool
	enableHTTP2          bool
	enableLeaderElection bool
	driftEvery           time.Duration
	zap                  zap.Options
}

// parseOptions reads the command line.
func parseOptions() options {
	opts := options{
		zap: zap.Options{
			Development: true,
		},
	}
	flag.StringVar(&opts.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&opts.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&opts.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&opts.metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&opts.metricsCertName, "metrics-cert-name", "tls.crt",
		"The name of the metrics server certificate file.")
	flag.StringVar(&opts.metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&opts.enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics server.")
	flag.DurationVar(&opts.driftEvery, "drift-interval", 5*time.Minute,
		"How long after a reconcile each bucket is checked again for a change made outside the cluster. 0 checks only "+
			"when the resource changes.")
	opts.zap.BindFlags(flag.CommandLine)
	flag.Parse()
	return opts
}

// main runs the manager until the process is stopped, and exits with 1 if it cannot.
func main() {
	opts := parseOptions()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts.zap)))
	if err := run(opts); err != nil {
		setupLog.Error(err, "the manager stopped")
		os.Exit(1)
	}
}

// run builds the manager and its controller, and runs them until the process is stopped.
func run(opts options) error {
	metricsOptions, metricsCertWatcher, err := metricsOptionsOf(opts)
	if err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOptions,
		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.enableLeaderElection,
		LeaderElectionID:       "794717dc.example.com",
	})
	if err != nil {
		return fmt.Errorf("start the manager: %w", err)
	}
	if err := setUpReconciler(mgr, opts); err != nil {
		return err
	}
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding the metrics certificate watcher to the manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			return fmt.Errorf("add the metrics certificate watcher: %w", err)
		}
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("set up the health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("set up the ready check: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("run the manager: %w", err)
	}
	return nil
}

// setUpReconciler makes the S3 client from the AWS environment and registers the reconciler with the manager.
func setUpReconciler(mgr ctrl.Manager, opts options) error {
	awsConfig, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return fmt.Errorf("load the AWS configuration: %w", err)
	}
	reconciler := &controller.ManagedBucketReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Buckets: &s3bucket.Client{
			Config: awsConfig,
			// An S3 emulator, which AWS_ENDPOINT_URL names, takes the bucket in the path and not in the host name.
			PathStyle: os.Getenv("AWS_ENDPOINT_URL") != "",
		},
		Recorder:   mgr.GetEventRecorderFor("managedbucket"),
		DriftEvery: opts.driftEvery,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("create the ManagedBucket controller: %w", err)
	}
	return nil
}

// metricsOptionsOf returns the options of the metrics server, and the watcher of its certificate if the options name
// one. Without a certificate, controller-runtime makes a self-signed one, which suits development and testing.
func metricsOptionsOf(opts options) (metricsserver.Options, *certwatcher.CertWatcher, error) {
	var tlsOpts []func(*tls.Config)
	if !opts.enableHTTP2 {
		// HTTP/2 stays off unless it is asked for, as a protection against the Stream Cancellation and Rapid Reset
		// attacks. See https://github.com/advisories/GHSA-qppj-fm5r-hxr3 and
		// https://github.com/advisories/GHSA-4374-p667-p6c8.
		tlsOpts = append(tlsOpts, func(config *tls.Config) {
			setupLog.Info("disabling http/2")
			config.NextProtos = []string{"http/1.1"}
		})
	}
	metricsOptions := metricsserver.Options{
		BindAddress:   opts.metricsAddr,
		SecureServing: opts.secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if opts.secureMetrics {
		// Only users and service accounts that RBAC allows can read the metrics.
		metricsOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	if len(opts.metricsCertPath) == 0 {
		return metricsOptions, nil, nil
	}
	setupLog.Info("Initializing the metrics certificate watcher using the provided certificates",
		"metrics-cert-path", opts.metricsCertPath, "metrics-cert-name", opts.metricsCertName,
		"metrics-cert-key", opts.metricsCertKey)
	watcher, err := certwatcher.New(
		filepath.Join(opts.metricsCertPath, opts.metricsCertName),
		filepath.Join(opts.metricsCertPath, opts.metricsCertKey),
	)
	if err != nil {
		return metricsserver.Options{}, nil, fmt.Errorf("initialize the metrics certificate watcher: %w", err)
	}
	metricsOptions.TLSOpts = append(metricsOptions.TLSOpts, func(config *tls.Config) {
		config.GetCertificate = watcher.GetCertificate
	})
	return metricsOptions, watcher, nil
}
