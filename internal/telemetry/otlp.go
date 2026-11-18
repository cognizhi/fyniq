package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// OTLP sends spans in the OpenTelemetry protocol's JSON encoding over HTTP: to Cloud Trace's Telemetry API in the release
// build, to a local collector otherwise. It needs no protobuf or gRPC library in the image (NFR-IMG budgets).
type OTLP struct {
	URL    string       // the traces endpoint, such as https://telemetry.googleapis.com/v1/traces
	Client *http.Client // through the egress guard
	// Token, when set, is the bearer token each request carries: the service's own account's, in the cloud.
	Token func(ctx context.Context) (string, error)
}

var _ sdktrace.SpanExporter = (*OTLP)(nil)

type (
	otlpValue struct {
		String *string     `json:"stringValue,omitempty"`
		Int    *string     `json:"intValue,omitempty"` // int64 as a string, as protobuf's JSON mapping has it
		Double *float64    `json:"doubleValue,omitempty"`
		Bool   *bool       `json:"boolValue,omitempty"`
		Array  *otlpValues `json:"arrayValue,omitempty"`
	}
	otlpValues struct {
		Values []otlpValue `json:"values"`
	}
	otlpKV struct {
		Key   string    `json:"key"`
		Value otlpValue `json:"value"`
	}
	otlpEvent struct {
		Time       string   `json:"timeUnixNano"`
		Name       string   `json:"name"`
		Attributes []otlpKV `json:"attributes,omitempty"`
	}
	otlpStatus struct {
		Code    int    `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	}
	otlpSpan struct {
		TraceID    string      `json:"traceId"`
		SpanID     string      `json:"spanId"`
		ParentID   string      `json:"parentSpanId,omitempty"`
		Name       string      `json:"name"`
		Kind       int         `json:"kind"`
		Start      string      `json:"startTimeUnixNano"`
		End        string      `json:"endTimeUnixNano"`
		Attributes []otlpKV    `json:"attributes,omitempty"`
		Events     []otlpEvent `json:"events,omitempty"`
		Status     otlpStatus  `json:"status"`
	}
	otlpScope struct {
		Scope struct {
			Name    string `json:"name"`
			Version string `json:"version,omitempty"`
		} `json:"scope"`
		Spans []otlpSpan `json:"spans"`
	}
	otlpResource struct {
		Resource struct {
			Attributes []otlpKV `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []otlpScope `json:"scopeSpans"`
	}
	otlpRequest struct {
		ResourceSpans []otlpResource `json:"resourceSpans"`
	}
)

func value(v attribute.Value) otlpValue {
	switch v.Type() {
	case attribute.BOOL:
		b := v.AsBool()
		return otlpValue{Bool: &b}
	case attribute.INT64:
		i := strconv.FormatInt(v.AsInt64(), 10)
		return otlpValue{Int: &i}
	case attribute.FLOAT64:
		f := v.AsFloat64()
		return otlpValue{Double: &f}
	case attribute.BOOLSLICE, attribute.INT64SLICE, attribute.FLOAT64SLICE, attribute.STRINGSLICE:
		var out otlpValues
		switch v.Type() {
		case attribute.BOOLSLICE:
			for _, b := range v.AsBoolSlice() {
				out.Values = append(out.Values, value(attribute.BoolValue(b)))
			}
		case attribute.INT64SLICE:
			for _, i := range v.AsInt64Slice() {
				out.Values = append(out.Values, value(attribute.Int64Value(i)))
			}
		case attribute.FLOAT64SLICE:
			for _, f := range v.AsFloat64Slice() {
				out.Values = append(out.Values, value(attribute.Float64Value(f)))
			}
		default:
			for _, s := range v.AsStringSlice() {
				out.Values = append(out.Values, value(attribute.StringValue(s)))
			}
		}
		return otlpValue{Array: &out}
	default:
		s := v.Emit()
		return otlpValue{String: &s}
	}
}

func kvs(attrs []attribute.KeyValue) []otlpKV {
	out := make([]otlpKV, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, otlpKV{Key: string(a.Key), Value: value(a.Value)})
	}
	return out
}

// sent are the only span attributes that leave the service: what was asked and how it ended, and Fyniq's own IDs.
// The client's address, its browser, the full URL and any header stay behind: a trace holds no personal data
// (PDPA; CTL-DP-07 [verify where Cloud Trace keeps traces]).
var sent = map[attribute.Key]bool{"http.request.method": true, "http.response.status_code": true, "http.route": true, "url.scheme": true,
	"server.address": true, "server.port": true, "network.protocol.version": true, "error.type": true}

// spanKVs are a span's attributes that may leave it.
func spanKVs(attrs []attribute.KeyValue) []otlpKV {
	var keep []attribute.KeyValue
	for _, a := range attrs {
		if sent[a.Key] || strings.HasPrefix(string(a.Key), "fyniq.") {
			keep = append(keep, a)
		}
	}
	return kvs(keep)
}

func nanos(t interface{ UnixNano() int64 }) string { return strconv.FormatInt(t.UnixNano(), 10) }

// statusCode is OTLP's code for a span's status: Ok 1, Error 2 (Go's codes number them the other way).
func statusCode(c codes.Code) int {
	switch c {
	case codes.Ok:
		return 1
	case codes.Error:
		return 2
	}
	return 0
}

// Request is the OTLP/JSON body of spans: grouped by their resource and instrumentation scope.
func Request(spans []sdktrace.ReadOnlySpan) ([]byte, error) {
	var req otlpRequest
	resources := map[string]int{}
	scopes := map[[2]string]int{}
	for _, s := range spans {
		res := s.Resource().Encoded(attribute.DefaultEncoder())
		ri, ok := resources[res]
		if !ok {
			var r otlpResource
			r.Resource.Attributes = kvs(s.Resource().Attributes())
			req.ResourceSpans = append(req.ResourceSpans, r)
			ri = len(req.ResourceSpans) - 1
			resources[res] = ri
		}
		key := [2]string{res, s.InstrumentationScope().Name + "@" + s.InstrumentationScope().Version}
		si, ok := scopes[key]
		if !ok {
			var sc otlpScope
			sc.Scope.Name, sc.Scope.Version = s.InstrumentationScope().Name, s.InstrumentationScope().Version
			req.ResourceSpans[ri].ScopeSpans = append(req.ResourceSpans[ri].ScopeSpans, sc)
			si = len(req.ResourceSpans[ri].ScopeSpans) - 1
			scopes[key] = si
		}
		span := otlpSpan{TraceID: s.SpanContext().TraceID().String(), SpanID: s.SpanContext().SpanID().String(), Name: s.Name(),
			Kind: int(s.SpanKind()), Start: nanos(s.StartTime()), End: nanos(s.EndTime()), Attributes: spanKVs(s.Attributes()),
			Status: otlpStatus{Code: statusCode(s.Status().Code), Message: s.Status().Description}}
		if s.Parent().IsValid() {
			span.ParentID = s.Parent().SpanID().String()
		}
		for _, e := range s.Events() {
			span.Events = append(span.Events, otlpEvent{Time: nanos(e.Time), Name: e.Name, Attributes: spanKVs(e.Attributes)})
		}
		sc := &req.ResourceSpans[ri].ScopeSpans[si]
		sc.Spans = append(sc.Spans, span)
	}
	return json.Marshal(req)
}

// ExportSpans implements sdktrace.SpanExporter.
func (o *OTLP) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if len(spans) == 0 {
		return nil
	}
	body, err := Request(spans)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(context.WithValue(ctx, untraced{}, true), http.MethodPost, o.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	if o.Token != nil {
		tok, err := o.Token(ctx)
		if err != nil {
			return fmt.Errorf("the traces' token: %w", err)
		}
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := o.Client.Do(r)
	if err != nil {
		return fmt.Errorf("send %d span(s): %w", len(spans), err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("send %d span(s): %s: %s", len(spans), res.Status, bytes.TrimSpace(msg))
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

// Shutdown implements sdktrace.SpanExporter.
func (o *OTLP) Shutdown(context.Context) error { return nil }
