package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// JobRequest asks a job runner to run a platform job's slot.
type JobRequest struct {
	Job  string    `json:"job"`
	Slot time.Time `json:"slot"`
}

// JobsHandler is the local stand-in for Cloud Run jobs (§12.12: a one-off run): it accepts a job
// with the jobs class's credential, answers 202 at once, and runs the job in the background, which
// reports completion to its slot and beats (RunSlot). In production the same job functions run in
// the jobs image as a Cloud Run job execution.
type JobsHandler struct {
	Tokens   ports.TaskTokens
	Platform storage.Platform
	Clock    ports.Clock
	Jobs     map[string]func(ctx context.Context) error
}

func (h *JobsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if err := h.Tokens.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), string(storage.ClassJobs)); err != nil {
		http.Error(w, "the jobs class does not accept that credential", http.StatusForbidden)
		return
	}
	var req JobRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
		http.Error(w, "not a job request", http.StatusBadRequest)
		return
	}
	run, ok := h.Jobs[req.Job]
	if !ok {
		http.Error(w, fmt.Sprintf("no job %q here", req.Job), http.StatusNotFound)
		return
	}
	go func() { _ = RunSlot(context.Background(), h.Platform, h.Clock, req.Job, req.Slot, run) }()
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, "local-%s-%d", req.Job, req.Slot.Unix())
}
