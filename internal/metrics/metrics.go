// Package metrics provides a Prometheus registry for service instrumentation.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds all metrics for the service.
type Registry struct {
	conversions *prometheus.CounterVec
	inFlight    prometheus.Gauge
	duration    prometheus.Histogram
	handler     http.Handler
}

// New returns a Registry backed by a fresh prometheus.Registry (never the
// default global registry, to avoid auto-registering Go runtime metrics).
func New() *Registry {
	reg := prometheus.NewRegistry()

	conversions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "docpdf_conversions_total",
		Help: "Total conversion attempts by outcome.",
	}, []string{"outcome"})

	inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "docpdf_conversions_in_flight",
		Help: "Current number of conversions in progress.",
	})

	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "docpdf_conversion_duration_ms",
		Help:    "Conversion duration in milliseconds.",
		Buckets: []float64{100, 250, 500, 1000, 2500, 5000, 10000, 30000},
	})

	reg.MustRegister(conversions, inFlight, duration)

	// Pre-initialize all outcome label values so they appear at zero in the
	// exposition even before any conversions have occurred.
	for _, outcome := range []string{"success", "timeout", "failed"} {
		conversions.WithLabelValues(outcome)
	}

	return &Registry{
		conversions: conversions,
		inFlight:    inFlight,
		duration:    duration,
		handler:     promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	}
}

// IncSuccess increments the successful conversion counter.
func (r *Registry) IncSuccess() { r.conversions.WithLabelValues("success").Inc() }

// IncTimeout increments the timed-out conversion counter.
func (r *Registry) IncTimeout() { r.conversions.WithLabelValues("timeout").Inc() }

// IncFailed increments the failed conversion counter.
func (r *Registry) IncFailed() { r.conversions.WithLabelValues("failed").Inc() }

// IncInFlight increments the in-flight conversion gauge.
func (r *Registry) IncInFlight() { r.inFlight.Inc() }

// DecInFlight decrements the in-flight conversion gauge.
func (r *Registry) DecInFlight() { r.inFlight.Dec() }

// ObserveDuration records a conversion duration in milliseconds.
func (r *Registry) ObserveDuration(ms int64) { r.duration.Observe(float64(ms)) }

// ServeHTTP serves the Prometheus text exposition (with content negotiation).
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}
