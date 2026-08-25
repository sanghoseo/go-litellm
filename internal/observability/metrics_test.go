package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsRecordsRequestAndExportsPrometheusFormat(t *testing.T) {
	metrics := NewMetrics()
	handler := metrics.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusCreated) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `litellm_proxy_http_requests_total{method="POST",path="/v1/chat/completions",status="201"} 1`) {
		t.Fatalf("metrics = %s", response.Body.String())
	}
}
