package postgres

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The registered private endpoints (SEC-37; migration 000310): a global table, written only through
// register_private_endpoint in a unit of work of the installation organisation, whose trail records each version, and read
// by an organisation for its own credentials through private_endpoints_for.

func toPrivateEndpoint(r sqlcdb.FyniqPrivateEndpoint) storage.PrivateEndpoint {
	return storage.PrivateEndpoint{ID: r.ID, Version: int(r.Version), Address: r.Address, Port: int(r.Port), Pin: r.TlsPin, Service: r.Service,
		Serves: r.Serves, Credential: r.Credential, Retired: r.Retired, Reason: r.Reason, RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt,
		Change: r.ChangeID}
}

func (u *unitOfWork) PrivateEndpoints() storage.PrivateEndpoints { return privateEndpoints{u} }

type privateEndpoints struct{ u *unitOfWork }

func (r privateEndpoints) List(ctx context.Context) ([]storage.PrivateEndpoint, error) {
	if err := (passkeys{r.u}).installation(ctx); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PrivateEndpoints(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PrivateEndpoint, 0, len(rows))
	for _, x := range rows {
		out = append(out, toPrivateEndpoint(x))
	}
	return out, nil
}

func (r privateEndpoints) For(ctx context.Context, service string, credential uuid.UUID) ([]storage.PrivateEndpoint, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PrivateEndpointsFor(ctx, sqlcdb.PrivateEndpointsForParams{Service: service, Credential: credential})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PrivateEndpoint, 0, len(rows))
	for _, x := range rows {
		out = append(out, toPrivateEndpoint(x))
	}
	return out, nil
}

func (r privateEndpoints) Register(ctx context.Context, by uuid.UUID, change *uuid.UUID, c storage.PrivateEndpointChange,
	at time.Time) (storage.PrivateEndpoint, error) {
	if err := (passkeys{r.u}).installation(ctx); err != nil { // the installation's trail records each version
		return storage.PrivateEndpoint{}, err
	}
	if c.Version < 0 || c.Version >= math.MaxInt32 || c.Port < 0 || c.Port > math.MaxInt32 {
		return storage.PrivateEndpoint{}, &storage.Error{Code: storage.CodeVersion, Message: "the endpoint's version or port is out of range"}
	}
	before := storage.PrivateEndpoint{}
	all, err := r.List(ctx)
	if err != nil {
		return storage.PrivateEndpoint{}, err
	}
	for _, e := range all {
		if e.ID == c.ID {
			before = e
		}
	}
	version, err := r.u.q.RegisterPrivateEndpoint(ctx, sqlcdb.RegisterPrivateEndpointParams{ByUser: by, ChangeID: change, ID: c.ID,
		Version: int32(c.Version), Address: c.Address, Port: int32(c.Port), Pin: c.Pin, Service: c.Service, Serves: c.Serves, // #nosec G115 -- checked above
		Credential: c.Credential, Retired: c.Retired, Reason: c.Reason, At: at})
	if err != nil {
		return storage.PrivateEndpoint{}, mapError(err)
	}
	if all, err = r.List(ctx); err != nil {
		return storage.PrivateEndpoint{}, err
	}
	var e storage.PrivateEndpoint
	for _, x := range all {
		if x.ID == c.ID {
			e = x
		}
	}
	str := func(p storage.PrivateEndpoint) string {
		if !p.Address.IsValid() {
			return ""
		}
		return p.Address.String()
	}
	changes := map[string][2]any{"address": {str(before), str(e)}, "port": {before.Port, e.Port}, "tls_pin": {before.Pin, e.Pin},
		"service": {before.Service, e.Service}, "serves": {idOrNil(before.Serves), e.Serves.String()},
		"credential": {idOrNil(before.Credential), e.Credential.String()}, "retired": {before.Retired, e.Retired}, "reason": {before.Reason, e.Reason}}
	if change != nil {
		changes["change_id"] = [2]any{nil, change.String()}
	}
	action := "private_endpoint.registered"
	switch {
	case e.Retired:
		action = "private_endpoint.retired"
	case c.Version > 0:
		action = "private_endpoint.changed"
	}
	v := int(version)
	return e, r.u.event(as(ctx, by), action, "private_endpoint", e.ID, &v, changes)
}

// idOrNil is an ID as text, or nil before there is one.
func idOrNil(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id.String()
}
