package metrics_test

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/BRO3886/go-docpdf/internal/metrics"
)

func TestCounters(t *testing.T) {
	reg := metrics.New()
	reg.IncSuccess()
	reg.IncSuccess()
	reg.IncTimeout()
	reg.IncFailed()

	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))

	body := w.Body.String()
	if !strings.Contains(body, `docpdf_conversions_total{outcome="success"} 2`) {
		t.Errorf("expected success=2 in output, got:\n%s", body)
	}
	if !strings.Contains(body, `docpdf_conversions_total{outcome="timeout"} 1`) {
		t.Errorf("expected timeout=1 in output, got:\n%s", body)
	}
	if !strings.Contains(body, `docpdf_conversions_total{outcome="failed"} 1`) {
		t.Errorf("expected failed=1 in output, got:\n%s", body)
	}
}

func TestInFlight(t *testing.T) {
	reg := metrics.New()
	reg.IncInFlight()
	reg.IncInFlight()
	reg.DecInFlight()

	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))

	body := w.Body.String()
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

	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()

	// bucket le=100: only 50ms observation
	if !strings.Contains(body, `docpdf_conversion_duration_ms_bucket{le="100"} 1`) {
		t.Errorf("expected bucket 100=1, got:\n%s", body)
	}
	// bucket le=250: 50+200
	if !strings.Contains(body, `docpdf_conversion_duration_ms_bucket{le="250"} 2`) {
		t.Errorf("expected bucket 250=2, got:\n%s", body)
	}
	// bucket le=1000: 50+200+600
	if !strings.Contains(body, `docpdf_conversion_duration_ms_bucket{le="1000"} 3`) {
		t.Errorf("expected bucket 1000=3, got:\n%s", body)
	}
	// bucket le=+Inf: all 4
	if !strings.Contains(body, `docpdf_conversion_duration_ms_bucket{le="+Inf"} 4`) {
		t.Errorf("expected bucket +Inf=4, got:\n%s", body)
	}
	// sum = 50+200+600+3000 = 3850
	if !strings.Contains(body, "docpdf_conversion_duration_ms_sum 3850") {
		t.Errorf("expected sum=3850, got:\n%s", body)
	}
	// count = 4
	if !strings.Contains(body, "docpdf_conversion_duration_ms_count 4") {
		t.Errorf("expected count=4, got:\n%s", body)
	}
}

func TestContentType(t *testing.T) {
	reg := metrics.New()
	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	ct := w.Header().Get("Content-Type")
	if ct != "text/plain; version=0.0.4; charset=utf-8" {
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

	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), `docpdf_conversions_total{outcome="success"} 50`) {
		t.Errorf("expected 50 successes after concurrent run, got:\n%s", w.Body.String())
	}
}
