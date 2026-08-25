package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Metrics struct {
	mu       sync.Mutex
	requests map[string]uint64
	duration map[string]float64
}

func NewMetrics() *Metrics {
	return &Metrics{requests: map[string]uint64{}, duration: map[string]float64{}}
}

func (metrics *Metrics) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		response := &statusWriter{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(response, request)
		key := request.Method + " " + request.URL.Path + " " + fmt.Sprint(response.status)
		metrics.mu.Lock()
		metrics.requests[key]++
		metrics.duration[key] += time.Since(startedAt).Seconds()
		metrics.mu.Unlock()
	})
}

func (metrics *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		metrics.mu.Lock()
		defer metrics.mu.Unlock()
		keys := make([]string, 0, len(metrics.requests))
		for key := range metrics.requests {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = writer.Write([]byte("# TYPE litellm_proxy_http_requests_total counter\n# TYPE litellm_proxy_http_request_duration_seconds_total counter\n"))
		for _, key := range keys {
			parts := strings.SplitN(key, " ", 3)
			_, _ = fmt.Fprintf(writer, "litellm_proxy_http_requests_total{method=%q,path=%q,status=%q} %d\n", parts[0], parts[1], parts[2], metrics.requests[key])
			_, _ = fmt.Fprintf(writer, "litellm_proxy_http_request_duration_seconds_total{method=%q,path=%q,status=%q} %.9f\n", parts[0], parts[1], parts[2], metrics.duration[key])
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}
func (writer *statusWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
