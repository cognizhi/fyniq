// Package telemetry traces Fyniq's requests with OpenTelemetry (NFR-OBS-01; PRD-001 rev 0.249, p1-operations-dr design
// D5): one tracer provider per binary, W3C trace context into and out of every service, a tenth of the requests kept,
// and every request that failed with a 5xx kept whole whatever the sampling said. Spans carry the organisation, the
// client and the request's ID, never a value of the request. Where they go is the exporter's: Cloud Trace in the
// release build, an OTLP endpoint locally when one is set, nowhere otherwise.
package telemetry

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Ratio is the share of requests traced: the owner's decision of 11 Nov 2026 (rev 0.249).
const Ratio = 0.1

// Config sets up a binary's tracing.
type Config struct {
	Service, Version string
	// Exporter receives the kept spans; nil keeps none, while trace context still flows through.
	Exporter sdktrace.SpanExporter
	// Ratio is the share of requests kept; 0 means Ratio. Failed requests are kept whatever it is.
	Ratio float64
	// Attributes describe where the binary runs (the cloud project, the region).
	Attributes []attribute.KeyValue
}

// Setup installs the tracer provider and W3C trace context for the whole binary, and returns what flushes and stops
// them.
func Setup(c Config) func(context.Context) error {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if c.Exporter == nil {
		return func(context.Context) error { return nil }
	}
	ratio := c.Ratio
	if ratio <= 0 {
		ratio = Ratio
	}
	attrs := append([]attribute.KeyValue{attribute.String("service.name", c.Service), attribute.String("service.version", c.Version)}, c.Attributes...)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(KeepFailures(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)))),
		sdktrace.WithSpanProcessor(NewTail(sdktrace.NewBatchSpanProcessor(c.Exporter))),
		sdktrace.WithResource(resource.NewSchemaless(attrs...)),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown
}

// KeepFailures records every span the sampler would drop, so the tail can still keep a request that fails; only the
// sampler's own choice is marked sampled and travels on as such.
func KeepFailures(s sdktrace.Sampler) sdktrace.Sampler { return keepFailures{s} }

type keepFailures struct{ s sdktrace.Sampler }

func (k keepFailures) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	r := k.s.ShouldSample(p)
	if r.Decision == sdktrace.Drop {
		r.Decision = sdktrace.RecordOnly
	}
	return r
}

func (k keepFailures) Description() string { return "KeepFailures{" + k.s.Description() + "}" }

// The tail's bounds: how many unsampled requests it holds at once, and how many spans of each, so a burst never grows
// memory without end. Beyond them a request's spans are dropped as the sampler meant.
const (
	tailTraces = 4096
	tailSpans  = 256
)

// Tail passes sampled spans on at once, and holds an unsampled request's spans until its local root ends: they go on
// when the root failed (a 5xx sets its status to Error), and are dropped otherwise. A span that ends after its root is
// dropped too: the tail remembers the last requests it decided.
type Tail struct {
	next  sdktrace.SpanProcessor
	mu    sync.Mutex
	held  map[trace.TraceID][]sdktrace.ReadOnlySpan
	done  map[trace.TraceID]bool
	order []trace.TraceID // done's, oldest first, so it forgets the oldest
}

// NewTail returns a tail in front of next.
func NewTail(next sdktrace.SpanProcessor) *Tail {
	return &Tail{next: next, held: map[trace.TraceID][]sdktrace.ReadOnlySpan{}, done: map[trace.TraceID]bool{}}
}

// decided remembers that id's root ended, forgetting the oldest beyond tailTraces; t.mu is held.
func (t *Tail) decided(id trace.TraceID) {
	if len(t.order) >= tailTraces {
		delete(t.done, t.order[0])
		t.order = t.order[1:]
	}
	t.done[id] = true
	t.order = append(t.order, id)
}

// OnStart implements sdktrace.SpanProcessor.
func (t *Tail) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) { t.next.OnStart(ctx, s) }

// OnEnd implements sdktrace.SpanProcessor.
func (t *Tail) OnEnd(s sdktrace.ReadOnlySpan) {
	if s.SpanContext().IsSampled() {
		t.next.OnEnd(s)
		return
	}
	id := s.SpanContext().TraceID()
	root := !s.Parent().IsValid() || s.Parent().IsRemote()
	t.mu.Lock()
	spans, ok := t.held[id]
	if !root {
		if !t.done[id] && (ok || len(t.held) < tailTraces) && len(spans) < tailSpans {
			t.held[id] = append(spans, s)
		}
		t.mu.Unlock()
		return
	}
	delete(t.held, id)
	t.decided(id)
	t.mu.Unlock()
	if s.Status().Code != codes.Error {
		return
	}
	for _, c := range spans {
		t.next.OnEnd(kept{c})
	}
	t.next.OnEnd(kept{s})
}

// kept is a span the tail keeps: marked sampled, so the processors behind it export it as they export a sampled one.
type kept struct{ sdktrace.ReadOnlySpan }

func (k kept) SpanContext() trace.SpanContext {
	return k.ReadOnlySpan.SpanContext().WithTraceFlags(trace.FlagsSampled)
}

// Shutdown implements sdktrace.SpanProcessor.
func (t *Tail) Shutdown(ctx context.Context) error { return t.next.Shutdown(ctx) }

// ForceFlush implements sdktrace.SpanProcessor.
func (t *Tail) ForceFlush(ctx context.Context) error { return t.next.ForceFlush(ctx) }

// Middleware traces each request a service answers, from the trace context it came with, if any; the health probes
// are not traced.
func Middleware(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, service,
			otelhttp.WithSpanNameFormatter(spanName),
			otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" && r.URL.Path != "/readyz" }))
	}
}

// spanName is a request's span name: its route once the mux has matched one, its method until then.
func spanName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method
}

// Routed names a request's span by the route that answered it, which only the mux knows once it has matched.
func Routed(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if r.Pattern == "" {
			return
		}
		span := trace.SpanFromContext(r.Context())
		span.SetName(r.Pattern)
		route := r.Pattern
		if i := strings.IndexByte(route, ' '); i >= 0 {
			route = route[i+1:]
		}
		span.SetAttributes(attribute.String("http.route", route))
	})
}

// Caller names, on the request's span, who asked: the organisation, the client and the request's ID.
func Caller(ctx context.Context, organisation, client, requestID string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	var attrs []attribute.KeyValue
	for k, v := range map[string]string{"fyniq.organisation": organisation, "fyniq.client": client, "fyniq.request_id": requestID} {
		if v != "" {
			attrs = append(attrs, attribute.String(k, v))
		}
	}
	span.SetAttributes(attrs...)
}

// untraced marks the exporter's own requests, which are never traced: each export would make spans to export.
type untraced struct{}

// Transport traces each outbound request but the exporter's own; with propagate, it also sends the trace context, which only the platform's
// own services and Google's APIs are given, never a third party.
func Transport(base http.RoundTripper, propagate bool) http.RoundTripper {
	opts := []otelhttp.Option{otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Host }),
		otelhttp.WithFilter(func(r *http.Request) bool { return r.Context().Value(untraced{}) == nil })}
	if !propagate {
		opts = append(opts, otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator()))
	}
	return otelhttp.NewTransport(base, opts...)
}
