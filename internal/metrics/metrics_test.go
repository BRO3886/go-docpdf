package metrics_test

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/BRO3886/go-docpdf/internal/metrics"
)

// scrape calls ServeHTTP and returns the response body.
func scrape(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	return w.Body.String()
}

func TestCounters(t *testing.T) {
	reg := metrics.New()
	reg.IncSuccess()
	reg.IncSuccess()
	reg.IncTimeout()
	reg.IncFailed()

	body := scrape(t, reg)
	cases := []string{
		`docpdf_conversions_total{outcome="failed"} 1`,
		`docpdf_conversions_total{outcome="success"} 2`,
		`docpdf_conversions_total{outcome="timeout"} 1`,
	}
	for _, want := range cases {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in output:\n%s", want, body)
		}
	}
}

func TestInFlight(t *testing.T) {
	reg := metrics.New()
	reg.IncInFlight()
	reg.IncInFlight()
	reg.DecInFlight()

	body := scrape(t, reg)
	if !strings.Contains(body, "docpdf_conversions_in_flight 1") {
		t.Errorf("expected in_flight=1, got:\n%s", body)
	}
}

func TestHistogramBucketPlacement(t *testing.T) {
	reg := metrics.New()
	reg.ObserveDuration(50)   // ≤100
	reg.ObserveDuration(200)  // ≤250
	reg.ObserveDuration(600)  // ≤1000
	reg.ObserveDuration(3000) // ≤5000

	body := scrape(t, reg)
	cases := []string{
		`docpdf_conversion_duration_ms_bucket{le="100"} 1`,
		`docpdf_conversion_duration_ms_bucket{le="250"} 2`,
		`docpdf_conversion_duration_ms_bucket{le="1000"} 3`,
		`docpdf_conversion_duration_ms_bucket{le="+Inf"} 4`,
		`docpdf_conversion_duration_ms_sum 3850`,
		`docpdf_conversion_duration_ms_count 4`,
	}
	for _, want := range cases {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in output:\n%s", want, body)
		}
	}
}

func TestContentType(t *testing.T) {
	reg := metrics.New()
	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("unexpected Content-Type: %q", ct)
	}
}

func TestConcurrentRace(t *testing.T) {
	reg := metrics.New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			reg.IncInFlight()
			reg.ObserveDuration(int64(n * 10))
			reg.IncSuccess()
			reg.DecInFlight()
		}(i)
	}
	wg.Wait()

	body := scrape(t, reg)
	if !strings.Contains(body, `docpdf_conversions_total{outcome="success"} 50`) {
		t.Errorf("expected 50 successes after concurrent run, got:\n%s", body)
	}
}
