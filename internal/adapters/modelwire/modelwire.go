// Package modelwire holds what every model provider's adapter needs on the wire (PRD-001 AI-70, AI-79): tool names a
// provider accepts, the server-sent events of a streamed answer, and errors mapped to the port's and scrubbed of the
// credential before anyone logs or shows them.
package modelwire

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ToolNames maps the platform's tool names, such as reports.figure, to names every provider accepts (letters, digits,
// underscores and hyphens, at most 64) and back, for one request.
type ToolNames struct {
	wire map[string]string
	back map[string]string
}

var notWire = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// NewToolNames maps the tools' names, each to a distinct wire name.
func NewToolNames(tools []ports.ModelTool) ToolNames {
	n := ToolNames{wire: map[string]string{}, back: map[string]string{}}
	for _, t := range tools {
		w := notWire.ReplaceAllString(t.Name, "_")
		if len(w) > 60 {
			w = w[:60]
		}
		for i, base := 2, w; n.back[w] != ""; i++ {
			w = base + "_" + strconv.Itoa(i)
		}
		n.wire[t.Name], n.back[w] = w, t.Name
	}
	return n
}

// Wire is name as the provider sees it; a name no tool has is passed as it is.
func (n ToolNames) Wire(name string) string {
	if w, ok := n.wire[name]; ok {
		return w
	}
	return name
}

// Back is the platform's name of a tool the provider named; a name the request did not offer is passed as it is, and
// the tool loop refuses it.
func (n ToolNames) Back(wire string) string {
	if name, ok := n.back[wire]; ok {
		return name
	}
	return wire
}

// Events reads server-sent events, calling each with every event's name and data; an error from each stops it.
func Events(r io.Reader, each func(event string, data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var event string
	var data bytes.Buffer
	flush := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		err := each(event, bytes.TrimSuffix(data.Bytes(), []byte("\n")))
		event = ""
		data.Reset()
		return err
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			data.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

// Scrub is a provider's text made safe to log and show (AI-79, SEC-15): the credentials removed, then anything shaped
// like a token, a key or an NRIC, and cut to 300 characters.
func Scrub(msg string, secrets ...string) string {
	for _, s := range secrets {
		if len(s) >= 4 {
			msg = strings.ReplaceAll(msg, s, audit.Redacted)
		}
	}
	msg = audit.RedactText(strings.TrimSpace(msg))
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

// Error is a provider's refusal as the port's error, by its HTTP status: an unknown model is ports.ErrModelNotFound
// (read the list again, AI-81); a rate limit, an overload or a server error ports.ErrModelBusy (retry or fail over,
// AI-76); a refused credential ports.ErrModelAuth; anything else ports.ErrModelRequest. The provider's own words
// follow, scrubbed.
func Error(provider string, status int, kind, msg string, secrets ...string) error {
	var base error
	switch {
	case status == 404:
		base = ports.ErrModelNotFound
	case status == 401 || status == 403:
		base = ports.ErrModelAuth
	case status == 408 || status == 429 || status >= 500:
		base = ports.ErrModelBusy
	default:
		base = ports.ErrModelRequest
	}
	detail := strings.TrimSpace(strings.Join([]string{kind, Scrub(msg, secrets...)}, ": "))
	detail = strings.Trim(detail, ": ")
	if detail == "" {
		return fmt.Errorf("%s answered %d: %w", provider, status, base)
	}
	return fmt.Errorf("%s answered %d (%s): %w", provider, status, detail, base)
}

// ErrStream is a stream that ended without its last event.
var ErrStream = errors.New("the stream ended before the answer was whole")
