package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// provider installs a tracer provider keeping ratio of the requests, plus every failed one, into an in-memory exporter.
func provider(t *testing.T, ratio float64) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(KeepFailures(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)))),
		sdktrace.WithSpanProcessor(NewTail(sdktrace.NewSimpleSpanProcessor(exp))),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "api"))))
	before, beforeProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(before)
		otel.SetTextMapPropagator(beforeProp)
	})
	return exp
}

// service is a service's handler as app.Serve builds it: traced, its spans named by route, one inner span per request.
func service() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, span := otel.Tracer("test").Start(r.Context(), "work")
		span.End()
		Caller(r.Context(), "org-1", "web", "req-1")
		if r.PathValue("id") == "broken" {
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	return Middleware("api")(Routed(mux))
}

func get(t *testing.T, h http.Handler, path string, header http.Header) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		r.Header[k] = v
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
}

// A tenth of the requests is traced, and every request that failed with a 5xx whatever the sampling said, whole: its
// server span named by its route, with the organisation, client and request ID, and its inner spans. A request that
// passed and was not sampled leaves nothing, and the health probes are never traced (NFR-OBS-01; rev 0.249).
// req: NFR-OBS-01
func TestEveryFailedRequestIsKeptAndATenthOfTheRest(t *testing.T) {
	exp := provider(t, 1e-12) // as good as none sampled
	h := service()
	get(t, h, "/v1/things/fine", nil)
	get(t, h, "/healthz", nil)
	if got := exp.GetSpans(); len(got) != 0 {
		t.Fatalf("an unsampled request that passed left %d span(s)", len(got))
	}
	get(t, h, "/v1/things/broken", nil)
	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("a failed request's spans: %d, want the inner and the server span", len(spans))
	}
	server := spans[1]
	attrs := map[attribute.Key]string{}
	for _, a := range server.Attributes {
		attrs[a.Key] = a.Value.Emit()
	}
	if server.Name != "GET /v1/things/{id}" || attrs["http.route"] != "/v1/things/{id}" || attrs["fyniq.organisation"] != "org-1" ||
		attrs["fyniq.client"] != "web" || attrs["fyniq.request_id"] != "req-1" || server.SpanKind != trace.SpanKindServer {
		t.Errorf("the server span: %s %v", server.Name, attrs)
	}
	if spans[0].Name != "work" || spans[0].Parent.SpanID() != server.SpanContext.SpanID() {
		t.Errorf("the inner span: %s under %s", spans[0].Name, spans[0].Parent.SpanID())
	}

	exp.Reset()
	all := provider(t, 1)
	get(t, service(), "/v1/things/fine", nil)
	if len(all.GetSpans()) != 2 || len(exp.GetSpans()) != 0 {
		t.Errorf("with every request sampled: %d span(s)", len(all.GetSpans()))
	}
}

// The trace context a request arrives with is followed: a caller's sampled trace continues, so the render or scanner
// service's spans join the api's (W3C trace context).
// req: NFR-OBS-01
func TestATraceContinuesFromItsCaller(t *testing.T) {
	exp := provider(t, 1e-12)
	parent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	get(t, service(), "/v1/things/fine", http.Header{"Traceparent": {parent}})
	spans := exp.GetSpans()
	if len(spans) != 2 || spans[1].SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		spans[1].Parent.SpanID().String() != "00f067aa0ba902b7" || !spans[1].Parent.IsRemote() {
		t.Fatalf("the continued trace: %d span(s) %+v", len(spans), spans)
	}
}

// An outbound request is traced, and carries the trace context only to the platform's own services and Google's APIs,
// never to a third party (SEC-36's clients).
// req: NFR-OBS-01
func TestOutboundRequestsCarryTheTraceOnlyWithin(t *testing.T) {
	exp := provider(t, 1)
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = append(seen, r.Header.Get("Traceparent")) }))
	defer srv.Close()
	ctx, span := otel.Tracer("test").Start(context.Background(), "request")
	for _, propagate := range []bool{true, false} {
		c := &http.Client{Transport: Transport(http.DefaultTransport, propagate)}
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		res, err := c.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}
	span.End()
	if len(seen) != 2 || !strings.HasPrefix(seen[0], "00-"+span.SpanContext().TraceID().String()) || seen[1] != "" {
		t.Errorf("trace context sent: %q", seen)
	}
	if n := len(exp.GetSpans()); n != 3 {
		t.Errorf("spans: %d, want the request and its two calls", n)
	}
}

// Spans go out as OTLP/JSON: hex IDs, the kind and status OTLP numbers them by, 64-bit integers as strings, grouped by
// resource and scope, with the service's own token; the client's address, its browser and the full URL never leave.
// req: NFR-OBS-01
func TestSpansGoOutAsOTLPJSON(t *testing.T) {
	var body map[string]any
	var auth, kind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, kind = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "api"),
		attribute.String("gcp.project_id", "fyniq-prod"))))
	_, span := tp.Tracer("fyniq").Start(context.Background(), "GET /v1/things/{id}", trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.Int("http.response.status_code", 500), attribute.Bool("fyniq.retry", false), attribute.StringSlice("fyniq.tags", []string{"a"}),
			attribute.String("client.address", "203.0.113.7"), attribute.String("user_agent.original", "Mozilla/5.0"), attribute.String("url.full", "https://x/?token=t")))
	span.SetStatus(codes.Error, "failed")
	span.End()
	o := &OTLP{URL: srv.URL + "/v1/traces", Client: srv.Client(), Token: func(context.Context) (string, error) { return "service-token", nil }}
	if err := o.ExportSpans(context.Background(), exp.GetSpans().Snapshots()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer service-token" || kind != "application/json" {
		t.Errorf("headers: %q %q", auth, kind)
	}
	rs := body["resourceSpans"].([]any)[0].(map[string]any)
	s := rs["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)[0].(map[string]any)
	if len(s["traceId"].(string)) != 32 || len(s["spanId"].(string)) != 16 || s["kind"] != float64(2) || s["name"] != "GET /v1/things/{id}" {
		t.Errorf("the span: %v", s)
	}
	if st := s["status"].(map[string]any); st["code"] != float64(2) || st["message"] != "failed" {
		t.Errorf("the status: %v", st)
	}
	raw, _ := json.Marshal(s["attributes"])
	if !strings.Contains(string(raw), `{"key":"http.response.status_code","value":{"intValue":"500"}}`) ||
		!strings.Contains(string(raw), `{"key":"fyniq.retry","value":{"boolValue":false}}`) || !strings.Contains(string(raw), `"arrayValue":{"values":[{"stringValue":"a"}]}`) {
		t.Errorf("the attributes: %s", raw)
	}
	for _, kept := range []string{"203.0.113.7", "Mozilla", "token="} {
		if strings.Contains(string(raw), kept) {
			t.Errorf("a span carried %q out of the service: %s", kept, raw)
		}
	}
	res, _ := json.Marshal(rs["resource"])
	if !strings.Contains(string(res), `{"key":"gcp.project_id","value":{"stringValue":"fyniq-prod"}}`) {
		t.Errorf("the resource: %s", res)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "quota", http.StatusTooManyRequests) }))
	defer failing.Close()
	if err := (&OTLP{URL: failing.URL, Client: failing.Client()}).ExportSpans(context.Background(), exp.GetSpans().Snapshots()); err == nil ||
		!strings.Contains(err.Error(), "429") {
		t.Errorf("a refused export: %v", err)
	}
}
