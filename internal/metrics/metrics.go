// Package metrics provides a minimal Prometheus-compatible registry
// using only stdlib (sync/atomic). No external dependencies.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// histBuckets are the upper bounds (in milliseconds) for conversion duration.
var histBuckets = []int64{100, 250, 500, 1000, 2500, 5000, 10000, 30000}

// histogram tracks a duration distribution using atomic bucket counters.
// Buckets are cumulative (≤ le), matching Prometheus convention.
type histogram struct {
	counts [8]atomic.Int64 // one per histBuckets entry
	sum    atomic.Int64    // total ms (integer)
	total  atomic.Int64    // total observations
}

// observe records a single duration in milliseconds.
func (h *histogram) observe(ms int64) {
	h.sum.Add(ms)
	h.total.Add(1)
	for i, le := range histBuckets {
		if ms <= le {
			h.counts[i].Add(1)
		}
	}
}

// Registry holds all metrics for the service.
type Registry struct {
	convSuccess  atomic.Int64
	convTimeout  atomic.Int64
	convFailed   atomic.Int64
	convInFlight atomic.Int64
	hist         histogram
}

// New returns a zero-value Registry ready for use.
func New() *Registry {
	return &Registry{}
}

// IncSuccess increments the successful conversion counter.
func (r *Registry) IncSuccess() { r.convSuccess.Add(1) }

// IncTimeout increments the timed-out conversion counter.
func (r *Registry) IncTimeout() { r.convTimeout.Add(1) }

// IncFailed increments the failed conversion counter.
func (r *Registry) IncFailed() { r.convFailed.Add(1) }

// IncInFlight increments the in-flight conversion gauge.
func (r *Registry) IncInFlight() { r.convInFlight.Add(1) }

// DecInFlight decrements the in-flight conversion gauge.
func (r *Registry) DecInFlight() { r.convInFlight.Add(-1) }

// ObserveDuration records a conversion duration in milliseconds.
func (r *Registry) ObserveDuration(ms int64) { r.hist.observe(ms) }

// ServeHTTP renders Prometheus text format exposition.
func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	r.writeTo(w)
}

func (r *Registry) writeTo(w io.Writer) {
	// Counters
	fmt.Fprintf(w, "# HELP docpdf_conversions_total Total conversion attempts by outcome.\n")
	fmt.Fprintf(w, "# TYPE docpdf_conversions_total counter\n")
	fmt.Fprintf(w, "docpdf_conversions_total{outcome=\"success\"} %d\n", r.convSuccess.Load())
	fmt.Fprintf(w, "docpdf_conversions_total{outcome=\"timeout\"} %d\n", r.convTimeout.Load())
	fmt.Fprintf(w, "docpdf_conversions_total{outcome=\"failed\"} %d\n", r.convFailed.Load())

	// In-flight gauge
	fmt.Fprintf(w, "# HELP docpdf_conversions_in_flight Current number of conversions in progress.\n")
	fmt.Fprintf(w, "# TYPE docpdf_conversions_in_flight gauge\n")
	fmt.Fprintf(w, "docpdf_conversions_in_flight %d\n", r.convInFlight.Load())

	// Histogram
	fmt.Fprintf(w, "# HELP docpdf_conversion_duration_ms Conversion duration in milliseconds.\n")
	fmt.Fprintf(w, "# TYPE docpdf_conversion_duration_ms histogram\n")

	// Bucket counts are already cumulative (each observation increments all
	// buckets with le >= the observed value), so render them directly.
	for i, le := range histBuckets {
		fmt.Fprintf(w, "docpdf_conversion_duration_ms_bucket{le=\"%d\"} %d\n", le, r.hist.counts[i].Load())
	}
	fmt.Fprintf(w, "docpdf_conversion_duration_ms_bucket{le=\"+Inf\"} %d\n", r.hist.total.Load())
	fmt.Fprintf(w, "docpdf_conversion_duration_ms_sum %d\n", r.hist.sum.Load())
	fmt.Fprintf(w, "docpdf_conversion_duration_ms_count %d\n", r.hist.total.Load())
}
