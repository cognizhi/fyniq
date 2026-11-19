package convertservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

type tokens struct{}

func (tokens) Token(string) (string, error) { return "ok", nil }
func (tokens) Verify(token, class string) error {
	if token != "ok" || class != Class {
		return ports.ErrUnauthorised
	}
	return nil
}

// engine stands in for the converter beside the service: "photo" becomes a JPEG, "scan" a PNG, "damaged" is the file's
// own problem, anything else the engine's.
type engine struct{}

func (engine) Convert(_ context.Context, kind ports.ConvertKind, data []byte) (ports.Converted, error) {
	switch {
	case string(data) == "damaged":
		return ports.Converted{}, ports.ErrNotConvertible
	case string(data) == "photo" && kind == ports.ConvertHEICToJPEG:
		return ports.Converted{MediaType: "image/jpeg", Data: []byte("\xff\xd8jpeg")}, nil
	case string(data) == "scan" && kind == ports.ConvertPreviewPNG:
		return ports.Converted{MediaType: "image/png", Data: []byte("\x89PNGpng")}, nil
	}
	return ports.Converted{}, errors.New("the engine is down")
}

// The converter service converts for its callers only, through its client: a picture back, a file that is the problem
// (the caller learns it is not convertible), an engine that is down (an error, never a picture), a kind it does not
// know, and a caller without a token (FR-DOC-08, SEC-24; rev 0.292).
// req: FR-DOC-08
// asvs: V5.2.1
func TestTheConverterServiceConvertsForItsCallers(t *testing.T) {
	srv := httptest.NewServer(Handler{Engine: engine{}, Tokens: tokens{}})
	defer srv.Close()
	ctx := context.Background()
	client := &Client{URL: srv.URL, HTTP: srv.Client(), Token: func(context.Context) (string, error) { return "ok", nil }}
	if c, err := client.Convert(ctx, ports.ConvertHEICToJPEG, []byte("photo")); err != nil || c.MediaType != "image/jpeg" || string(c.Data) != "\xff\xd8jpeg" {
		t.Errorf("a photo: %+v %v", c, err)
	}
	if c, err := client.Convert(ctx, ports.ConvertPreviewPNG, []byte("scan")); err != nil || c.MediaType != "image/png" {
		t.Errorf("a preview: %+v %v", c, err)
	}
	if _, err := client.Convert(ctx, ports.ConvertHEICToJPEG, []byte("damaged")); !errors.Is(err, ports.ErrNotConvertible) {
		t.Errorf("a damaged file: %v", err)
	}
	var p *Problem
	if _, err := client.Convert(ctx, ports.ConvertHEICToJPEG, []byte("other")); !errors.As(err, &p) || p.Code != CodeFailed || strings.Contains(p.Message, "other") {
		t.Errorf("an engine that is down: %v", err)
	}
	if _, err := client.Convert(ctx, "nonsense", []byte("photo")); !errors.As(err, &p) || p.Code != CodeKind {
		t.Errorf("an unknown kind: %v", err)
	}
	// A caller with no token, and an answer that is not a picture.
	if _, err := (&Client{URL: srv.URL, HTTP: srv.Client(), Token: func(context.Context) (string, error) { return "no", nil }}).Convert(ctx, ports.ConvertHEICToJPEG, []byte("photo")); !errors.As(err, &p) || p.Code != CodeUnauthorised {
		t.Errorf("no token: %v", err)
	}
	text := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }))
	defer text.Close()
	if _, err := (&Client{URL: text.URL, HTTP: text.Client(), Token: func(context.Context) (string, error) { return "ok", nil }}).Convert(ctx, ports.ConvertHEICToJPEG, []byte("photo")); err == nil {
		t.Error("an answer that is not a picture was accepted")
	}
}
