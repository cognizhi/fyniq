package modelwire

import (
	"errors"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Tool names map to names every provider accepts and back, distinct even when two would collide; a name the request
// did not offer is passed as it is, for the tool loop to refuse.
// req: AI-70
func TestToolNames(t *testing.T) {
	n := NewToolNames([]ports.ModelTool{{Name: "reports.figure"}, {Name: "reports_figure"}, {Name: "kb.read"}, {Name: strings.Repeat("x", 80)}})
	if n.Wire("reports.figure") != "reports_figure" || n.Wire("reports_figure") != "reports_figure_2" || n.Wire("kb.read") != "kb_read" ||
		len(n.Wire(strings.Repeat("x", 80))) != 60 {
		t.Errorf("wire names: %v", n.wire)
	}
	for name, wire := range n.wire {
		if n.Back(wire) != name {
			t.Errorf("%s → %s → %s", name, wire, n.Back(wire))
		}
	}
	if n.Back("records_delete") != "records_delete" || n.Wire("unknown.tool") != "unknown.tool" {
		t.Error("a name the request did not offer was changed")
	}
}

// Server-sent events are read whole: named or not, with comments skipped and data over several lines joined.
// req: AI-70
func TestEvents(t *testing.T) {
	in := ": keep-alive\n\nevent: message_start\ndata: {\"a\":1}\n\ndata: line one\ndata: line two\n\nevent: last\ndata: x"
	var got []string
	if err := Events(strings.NewReader(in), func(event string, data []byte) error {
		got = append(got, event+"="+string(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != `message_start={"a":1}|=line one`+"\n"+`line two|last=x` {
		t.Errorf("events: %q", got)
	}
	stop := errors.New("stop")
	if err := Events(strings.NewReader(in), func(string, []byte) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("an error from each: %v", err)
	}
}

// A provider's refusal is the port's error by its status, its words scrubbed of the credential, tokens, keys and NRIC
// numbers, and cut short (AI-79, SEC-15).
// req: AI-79
func TestErrorsAreScrubbed(t *testing.T) {
	key := "sk-" + "live0123456789abcdefXYZ"
	err := Error("example", 400, "invalid_request_error", "the key "+key+" may not call this; Bearer abc.def.ghi for S1234567D", key)
	if !errors.Is(err, ports.ErrModelRequest) || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "abc.def.ghi") ||
		strings.Contains(err.Error(), "S1234567D") || !strings.Contains(err.Error(), "invalid_request_error") {
		t.Errorf("a scrubbed refusal: %v", err)
	}
	for status, want := range map[int]error{404: ports.ErrModelNotFound, 401: ports.ErrModelAuth, 403: ports.ErrModelAuth, 408: ports.ErrModelBusy,
		429: ports.ErrModelBusy, 500: ports.ErrModelBusy, 529: ports.ErrModelBusy, 422: ports.ErrModelRequest} {
		if err := Error("example", status, "", "", key); !errors.Is(err, want) {
			t.Errorf("%d: %v", status, err)
		}
	}
	if got := Scrub(strings.Repeat("a", 400)); len(got) > 310 {
		t.Errorf("a long message: %d", len(got))
	}
}
