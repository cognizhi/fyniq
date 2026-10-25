package bankfeed

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MaxBody is the largest webhook read: a prompt carries no data the platform keeps.
const MaxBody = 64 << 10

// Mux is where the receiver is mounted.
type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Hooks receives the bank-data connectors' webhooks at /webhooks/bank/{connector}/{id} (the proxy's /api prefix
// stripped; design D11): the installed connector its ID names is found before any tenant is set; an unknown one, one of
// another connector, or one disabled or uninstalled gets 404; its binding checks the signature against its own
// secrets (401 when it fails); a verified webhook queues an early sync of its connector, at most one a minute, and its
// body is never read for data.
type Hooks struct {
	Store      storage.Store
	Routes     storage.ConnectorRoutes
	Runtime    *connector.Runtime
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
	Log        *slog.Logger
}

// errSignature is a webhook its connector's binding refused.
var errSignature = errors.New("the webhook's signature does not verify")

// Register mounts the receiver.
func (h *Hooks) Register(mux Mux) {
	mux.HandleFunc("POST /webhooks/bank/{connector}/{id}", h.receive)
}

func (h *Hooks) receive(w http.ResponseWriter, r *http.Request) {
	notFound := func() { http.Error(w, "no such connector, or it takes no webhook now", http.StatusNotFound) }
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound()
		return
	}
	route, err := h.Routes.OrganisationConnectorRoute(r.Context(), id)
	if err != nil || route.Connector != r.PathValue("connector") || (route.State != storage.ConnectorEnabled && route.State != storage.ConnectorPaused) {
		notFound()
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
	if err != nil || len(body) > MaxBody {
		http.Error(w, "the request is too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	ctx := storage.WithAudit(r.Context(), storage.AuditContext{Actor: &storage.Actor{Type: "integration", ID: "connector/" + id.String()}})
	err = h.Store.Do(ctx, route.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := h.Runtime.VerifyWebhook(ctx, u, id, r.Header, body); err != nil {
			if errors.Is(err, connector.ErrNotEnabled) {
				return err
			}
			return errors.Join(errSignature, err)
		}
		return Queue(ctx, h.Dispatcher, u, route.Organisation, id, h.Clock.Now())
	})
	switch {
	case errors.Is(err, connector.ErrNotEnabled):
		notFound()
	case errors.Is(err, errSignature):
		http.Error(w, errSignature.Error(), http.StatusUnauthorized)
	case err != nil:
		if h.Log != nil {
			h.Log.ErrorContext(ctx, "bankfeed.webhook", "connector", id.String(), "error", err.Error())
		}
		http.Error(w, "the sync could not be queued; send it again", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}
