# PRD: Replace hand-rolled metrics with `prometheus/client_golang`

**Status:** Proposed
**Date:** 2026-02-25

---

## Problem

The current `internal/metrics` package is a hand-rolled Prometheus text format renderer backed by `sync/atomic`. It works for basic scraping but diverges from the Prometheus data model in ways that will cause friction when wiring into a real observability stack.

---

## Current State

`internal/metrics/metrics.go` — ~100 lines:

- 3 `atomic.Int64` counters (`convSuccess`, `convTimeout`, `convFailed`)
- 1 `atomic.Int64` gauge (`convInFlight`)
- 1 histogram: `[8]atomic.Int64` cumulative bucket array + sum + total
- `ServeHTTP` renders Prometheus text format (`text/plain; version=0.0.4`) via `fmt.Fprintf`

go.mod has **zero external dependencies**. go.sum is empty.

---

## Proposed Change

Replace `internal/metrics` with `github.com/prometheus/client_golang`, using a custom (non-default) registry to avoid auto-registering Go runtime metrics unless explicitly opted in.

### Dependency cost (measured, v1.23.2)

| Metric | Value |
|--------|-------|
| Direct dep | `github.com/prometheus/client_golang v1.23.2` |
| Entries in go.mod `require` block | 10 indirect deps |
| go.sum lines | 46 |
| Full module graph | 37 modules |
| Notable transitive deps | `google.golang.org/protobuf`, `golang.org/x/sys`, `github.com/beorn7/perks`, `github.com/cespare/xxhash/v2`, `github.com/prometheus/procfs` |

This is the primary cost. The "zero external dependencies" positioning in the README and the Reddit post would need to be updated.

---

## Tradeoffs

### In favour

**1. Label-based counters instead of three separate fields**

Current:
```go
type Registry struct {
    convSuccess  atomic.Int64
    convTimeout  atomic.Int64
    convFailed   atomic.Int64
    ...
}
// rendered as three separate metric series
```

With client_golang:
```go
conversions := prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "docpdf_conversions_total",
    Help: "Total conversions by outcome.",
}, []string{"outcome"})

conversions.WithLabelValues("success").Inc()
conversions.WithLabelValues("timeout").Inc()
```
One metric, proper label cardinality — PromQL, Grafana dashboards, and alerting rules all work without workarounds.

**2. Native histogram support (Prometheus 2.x)**

Prometheus 2.40+ introduced native histograms (exponential bucketing, no pre-declared buckets, higher resolution). The official client supports these via `prometheus.NewHistogram` with `NativeHistogramBucketFactor`. The hand-rolled version only supports classic fixed-bucket histograms and has no upgrade path.

**3. `_created` timestamps**

Prometheus OpenMetrics spec requires `_created` timestamps on counters and histograms to distinguish a reset counter (process restart) from a zero counter. The hand-rolled version omits these. Some Prometheus scrapers warn on their absence; future scrapers may require them.

**4. Content negotiation (Protobuf exposition)**

Prometheus server negotiates exposition format via `Accept` header. The official `promhttp.Handler` serves Protobuf when requested (more efficient for large metric sets) and falls back to text. The hand-rolled `ServeHTTP` always serves text.

**5. Go runtime metrics for free**

```go
reg.MustRegister(collectors.NewGoCollector())
reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
```
Adds goroutine count, GC pause histogram, heap stats, open FDs — all useful in production with no extra code.

**6. Histogram correctness edge cases**

The hand-rolled `observe(ms)` increments all buckets where `ms <= le`. This is correct for the cumulative storage convention but required a bug fix during implementation (double-accumulation in `writeTo`). The official client handles this internally and has been battle-tested across thousands of production deployments.

---

### Against

**1. 37-module dependency graph for 3 metrics**

The most honest objection. `google.golang.org/protobuf` alone is a significant pull for a service whose entire value proposition is simplicity. For a project with this few metrics and fixed cardinality, the hand-rolled version is demonstrably sufficient.

**2. Default registry auto-registers Go runtime metrics**

`prometheus.MustRegister(...)` uses the default global registry which already includes Go runtime and process collectors. If you use the default registry you get goroutine/GC/heap metrics without asking for them — fine in production, but it changes `GET /metrics` output in a way that may surprise. **Mitigation:** use a custom registry (`prometheus.NewRegistry()`) and register only what you want explicitly.

**3. `promhttp.Handler` wraps panics and adds its own logging**

`promhttp.HandlerFor(reg, promhttp.HandlerOpts{})` recovers panics in collectors and can log errors to a provided logger. This is useful but means the handler is no longer a trivial `fmt.Fprintf` — debugging metric rendering issues becomes more indirect.

**4. Module graph is larger than it looks**

`go list -m all` returns 37 modules. Most are test-only dependencies of the prometheus packages themselves and won't be compiled into the binary, but they appear in go.sum and must be downloaded during `go mod tidy`. In air-gapped or constrained CI environments this matters.

**5. Loss of "zero external dependencies" story**

This is a real positioning cost. The README, Reddit post, and CLAUDE.md all call this out. It's not a technical argument but it's a legitimate project identity question.

---

## Edge Cases to Handle During Migration

| Case | Detail |
|------|--------|
| Custom registry | Must use `prometheus.NewRegistry()` — never the default — to avoid auto-registered Go runtime metrics polluting `/metrics` unexpectedly |
| `_created` counter reset | client_golang emits `docpdf_conversions_total_created` timestamps; ensure scrape config doesn't choke on them if using an older Prometheus |
| Label value validation | `WithLabelValues("success")` panics at runtime if the label name doesn't match the vec declaration; use `GetMetricWithLabelValues` + check error in production code |
| Histogram bucket declaration | Must pre-declare buckets at init time; can't add buckets dynamically. Current buckets `{100, 250, 500, 1000, 2500, 5000, 10000, 30000}` are fine to carry over |
| Native histograms opt-in | `NativeHistogramBucketFactor` must be set explicitly; default is 0 (disabled). Don't enable unless Prometheus scraper is 2.40+ |
| `promhttp` content negotiation | If a Prometheus scraper sends `Accept: application/vnd.google.protobuf`, the handler will serve protobuf — ensure any intermediate proxy doesn't reject non-text responses on `/metrics` |
| Test assertions | Current tests assert against raw text output (string contains). With client_golang, use `testutil.ToFloat64(counter)` from `github.com/prometheus/client_golang/prometheus/testutil` for cleaner assertions |
| In-flight gauge | Replace `atomic.Int64` with `prometheus.NewGauge` + `.Inc()` / `.Dec()` — semantics identical, implementation cleaner |

---

## Migration Sketch

```go
// internal/metrics/metrics.go (after migration)

import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

type Registry struct {
    conversions prometheus.CounterVec  // label: outcome
    inFlight    prometheus.Gauge
    duration    prometheus.Histogram
    handler     http.Handler
}

func New() *Registry {
    reg := prometheus.NewRegistry()

    conversions := prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "docpdf_conversions_total",
        Help: "Total conversions by outcome.",
    }, []string{"outcome"})

    inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "docpdf_conversions_in_flight",
        Help: "Conversions currently in progress.",
    })

    duration := prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "docpdf_conversion_duration_ms",
        Help:    "Conversion duration in milliseconds.",
        Buckets: []float64{100, 250, 500, 1000, 2500, 5000, 10000, 30000},
    })

    reg.MustRegister(conversions, inFlight, duration)

    return &Registry{
        conversions: *conversions,
        inFlight:    inFlight,
        duration:    duration,
        handler:     promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
    }
}

func (r *Registry) IncSuccess()           { r.conversions.WithLabelValues("success").Inc() }
func (r *Registry) IncTimeout()           { r.conversions.WithLabelValues("timeout").Inc() }
func (r *Registry) IncFailed()            { r.conversions.WithLabelValues("failed").Inc() }
func (r *Registry) IncInFlight()          { r.inFlight.Inc() }
func (r *Registry) DecInFlight()          { r.inFlight.Dec() }
func (r *Registry) ObserveDuration(ms int64) { r.duration.Observe(float64(ms)) }
func (r *Registry) ServeHTTP(w http.ResponseWriter, r2 *http.Request) { r.handler.ServeHTTP(w, r2) }
```

The public API of `Registry` is unchanged — `middleware.go`, `handler.go`, and `main.go` require zero modifications.

---

## Backwards Compatibility

**This migration does not need to be backwards compatible.** go-docpdf is a Docker image published to GHCR, not a Go library. There are no importers, no public Go API to preserve, and no semver contract on the `/metrics` output format. Concretely:

- The `internal/metrics` package is unexported from the module — no external consumer can import it
- The `/metrics` HTTP response will change (label format, added `_created` timestamps, potentially protobuf support) — any existing scrape config pointing at the hand-rolled output will need to be updated, but this is a one-line Prometheus config change, not a breaking API change
- Deployers pulling `:latest` or a new semver tag from GHCR simply get the new image — no migration path needed for the old format
- The `Registry` public method signatures (`IncSuccess`, `ObserveDuration`, etc.) can be changed freely since they're only called from within this module

This removes the main implementation risk. The migration can be done in a single PR with no compatibility shim, no deprecation period, and no feature flag.

---

## Recommendation

**Do it** if:
- You plan to wire this into a real Prometheus + Grafana stack
- You want Go runtime metrics
- You anticipate adding more metrics with label dimensions

**Leave it hand-rolled** if:
- The zero-dep / lightweight positioning is a hard constraint
- The metric set stays fixed at these 3 instruments
- Scraping is done by a simple tool that only needs text format

The migration is low-risk (public API unchanged, ~100 lines swapped, no backwards compatibility requirement) and the dependency cost is real but one-time.
