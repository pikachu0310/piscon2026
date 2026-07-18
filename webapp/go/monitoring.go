package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	httppprof "net/http/pprof"
	"runtime"
	"runtime/metrics"
	runtimepprof "runtime/pprof"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

// routeLabelsActive is non-zero only while a CPU profile is being captured.
// Keeping this disabled outside a profile avoids allocating a label/context on
// every competition request merely because the diagnostics server exists.
var routeLabelsActive uint32

// profilingMiddleware adds only low-cardinality route templates to CPU and
// goroutine profiles. Never use the raw URL here: it contains UUIDs and would
// create a label for every request.
func profilingMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if atomic.LoadUint32(&routeLabelsActive) == 0 {
			return next(c)
		}

		route := c.Path()
		if route == "" {
			route = "unmatched"
		}

		req := c.Request()
		labels := runtimepprof.Labels("route", route, "method", req.Method)
		var handlerErr error
		runtimepprof.Do(req.Context(), labels, func(ctx context.Context) {
			c.SetRequest(req.WithContext(ctx))
			handlerErr = next(c)
		})
		return handlerErr
	}
}

func cpuProfileHandler(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint32(&routeLabelsActive, 1)
	defer atomic.AddUint32(&routeLabelsActive, ^uint32(0))
	httppprof.Profile(w, r)
}

// startDiagnosticsServer exposes machine-readable diagnostics on loopback.
// PROFILE_ADDR=off disables it. Do not bind this server to a public address:
// pprof output can reveal implementation details.
func startDiagnosticsServer(logger echo.Logger) {
	addr := getEnv("PROFILE_ADDR", "127.0.0.1:6060")
	if addr == "off" {
		logger.Info("diagnostics server is disabled")
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		logger.Errorf("diagnostics address must be loopback, got %q", addr)
		return
	}

	if rate := getEnvInt("BLOCK_PROFILE_RATE", 0); rate > 0 {
		runtime.SetBlockProfileRate(rate)
	}
	if fraction := getEnvInt("MUTEX_PROFILE_FRACTION", 0); fraction > 0 {
		runtime.SetMutexProfileFraction(fraction)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", cpuProfileHandler)
	mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)
	mux.HandleFunc("/debug/runtime-metrics", runtimeMetricsHandler)
	mux.HandleFunc("/debug/db-stats", dbStatsHandler)
	mux.HandleFunc("/debug/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Infof("diagnostics server is listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("diagnostics server failed: %v", err)
		}
	}()
}

func getEnvInt(key string, defaultValue int) int {
	value := getEnv(key, strconv.Itoa(defaultValue))
	n, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return n
}

type runtimeMetric struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Kind        string      `json:"kind"`
	Value       interface{} `json:"value"`
}

type runtimeHistogram struct {
	Counts  []uint64 `json:"counts"`
	Buckets []string `json:"buckets"`
}

func runtimeMetricsHandler(w http.ResponseWriter, _ *http.Request) {
	descriptions := metrics.All()
	samples := make([]metrics.Sample, len(descriptions))
	for i := range descriptions {
		samples[i].Name = descriptions[i].Name
	}
	metrics.Read(samples)

	result := make([]runtimeMetric, 0, len(samples))
	for i, sample := range samples {
		metric := runtimeMetric{
			Name:        sample.Name,
			Description: descriptions[i].Description,
		}
		switch sample.Value.Kind() {
		case metrics.KindUint64:
			metric.Kind = "uint64"
			metric.Value = sample.Value.Uint64()
		case metrics.KindFloat64:
			metric.Kind = "float64"
			metric.Value = sample.Value.Float64()
		case metrics.KindFloat64Histogram:
			histogram := sample.Value.Float64Histogram()
			buckets := make([]string, len(histogram.Buckets))
			for j, bucket := range histogram.Buckets {
				// JSON has no representation for +/-Inf, so encode every
				// boundary consistently as a decimal string.
				buckets[j] = strconv.FormatFloat(bucket, 'g', -1, 64)
			}
			metric.Kind = "float64_histogram"
			metric.Value = runtimeHistogram{
				Counts:  histogram.Counts,
				Buckets: buckets,
			}
		default:
			continue
		}
		result = append(result, metric)
	}

	writeJSON(w, http.StatusOK, result)
}

func dbStatsHandler(w http.ResponseWriter, _ *http.Request) {
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database is not ready"})
		return
	}

	stats := db.Stats()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"max_open_connections":  stats.MaxOpenConnections,
		"open_connections":      stats.OpenConnections,
		"in_use":                stats.InUse,
		"idle":                  stats.Idle,
		"wait_count":            stats.WaitCount,
		"wait_duration_seconds": stats.WaitDuration.Seconds(),
		"max_idle_closed":       stats.MaxIdleClosed,
		"max_idle_time_closed":  stats.MaxIdleTimeClosed,
		"max_lifetime_closed":   stats.MaxLifetimeClosed,
	})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = fmt.Fprintf(w, "{\"error\":%q}\n", err.Error())
	}
}
