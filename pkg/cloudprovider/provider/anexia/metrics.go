package anexia

import (
	"path"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	anxclient "go.anx.io/go-anxcloud/pkg/client"
)

const (
	metricsPrefix = "machine_controller_anexia_"
)

var (
	metricRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: metricsPrefix + "http_request_duration_seconds",
			Help: "Duration from sending a request to Anexia Engine to retrieving the response in seconds",
		},
		[]string{"resource", "method"},
	)

	metricRequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricsPrefix + "http_requests_total",
			Help: "Total amount of requests sent to Anexia Engine",
		},
		[]string{"resource", "method", "status"},
	)

	metricRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: metricsPrefix + "http_requests_in_flight",
			Help: "Amount of requests sent to Anexia Engine currently waiting for response",
		},
		[]string{"resource", "method"},
	)

	metricVMProvisioningDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricsPrefix + "vm_provisioning_duration_seconds",
			Help:    "Duration to provision a VM via Anexia Engine in seconds",
			Buckets: prometheus.ExponentialBuckets(0.1, 5, 7),
		},
		[]string{"location", "template"},
	)

	metricVMDeprovisioningDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricsPrefix + "vm_deprovisioning_duration_seconds",
			Help:    "Duration to deprovision a VM via Anexia Engine in seconds",
			Buckets: prometheus.ExponentialBuckets(0.1, 5, 7),
		},
		[]string{"location"},
	)
)

func init() {
	metrics.Registry.MustRegister(metricRequestDuration)
	metrics.Registry.MustRegister(metricRequestCount)
	metrics.Registry.MustRegister(metricRequestsInFlight)
	metrics.Registry.MustRegister(metricVMProvisioningDuration)
	metrics.Registry.MustRegister(metricVMDeprovisioningDuration)
}

func metricReceiver(metrics map[anxclient.Metric]float64, labels map[anxclient.MetricLabel]string) {
	var resource, method, status string

	for label, value := range labels {
		switch label {
		case anxclient.MetricLabelResource:
			resource = filterResourceLabel(value)
		case anxclient.MetricLabelMethod:
			method = value
		case anxclient.MetricLabelStatus:
			status = value
		}
	}

	for metric, value := range metrics {
		switch metric {
		case anxclient.MetricRequestDuration:
			metricRequestDuration.WithLabelValues(resource, method).Observe(value)
		case anxclient.MetricRequestCount:
			metricRequestCount.WithLabelValues(resource, method, status).Add(value)
		case anxclient.MetricRequestInflight:
			metricRequestsInFlight.WithLabelValues(resource, method).Add(value)
		}
	}
}

// filterResourceLabel takes the resource label given to the MetricReceiver by go-anxcloud client and tries to
// prevent swamping Prometheus with high-cardinality labels by
//  * removing the /api/ prefix (not high-cardinality relevant, but still nice)
//  * checking if the second to last path element ends with ".json", truncating the last path element in this case
//    - for metrics we do not care for the exact resource but the type of resource
//    - "it takes X seconds to retrieve VM infos"
//  * some resource-specific handling
//
// Having this here is if course not ideal, but it's the least-invasive way to add metrics to go-anxcloud and use
// them here. Once we have the new generic client in go-anxcloud for everything, this will get better as we then
// just generate metrics by Object type and Operation, not by URL.
func filterResourceLabel(resource string) string {
	resource = strings.TrimPrefix(resource, "/api/")

	if identifierRemoved := path.Base(path.Dir(resource)); strings.HasSuffix(identifierRemoved, ".json") {
		resource = identifierRemoved
	}

	// the vsphere info API endpoint is at "vsphere/v1/info.json/$identifier/info" for some reason, so the
	// identifier stripping above does not catch it
	const vsphereInfo = "vsphere/v1/info.json"
	if strings.HasPrefix(resource, vsphereInfo+"/") {
		resource = vsphereInfo
	}

	// the vsphere provisioning API endpoint is at
	// "vsphere/v1/provisioning/vm.json/$location/$template_type/$template", which again prevents the identifier
	// stripping above from catching it
	const vsphereProvisioning = "vsphere/v1/provisioning/vm.json"
	if strings.HasPrefix(resource, vsphereProvisioning+"/") {
		resource = vsphereProvisioning
	}

	return resource
}
