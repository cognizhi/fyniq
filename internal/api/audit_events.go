package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// ------------------------------------------------------------------ the audit trail explorer (FR-CTL-03, AUD-07)

// AuditActor is who acted, or on whose behalf.
type AuditActor struct {
	Type string `json:"type" enum:"user,service,integration,scheduler,ai,system" doc:"What kind of actor." example:"user"`
	ID   string `json:"id" doc:"The user's, service's or integration's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

// AuditFieldName is a field an event changed.
type AuditFieldName string

// Schema implements huma.SchemaProvider: a string, with an example.
func (AuditFieldName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A field of the record the event changed.", Examples: []any{"status"}}
}

// AuditApprovalID is an approval an event carried out.
type AuditApprovalID string

// Schema implements huma.SchemaProvider: a string, with an example.
func (AuditApprovalID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An approval's ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"}}
}

// AuditRecord is the record an event is about.
type AuditRecord struct {
	Type    string `json:"type" doc:"The record's type." example:"document"`
	ID      string `json:"id" format:"uuid" doc:"The record." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Version *int   `json:"version,omitempty" doc:"Its version the event made, for a versioned record." example:"3"`
}

// AuditEventSummary is an event in the explorer's list.
type AuditEventSummary struct {
	ID         string           `json:"id" format:"uuid" doc:"The event." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	Seq        int64            `json:"seq" doc:"Its place in the organisation's hash chain." example:"342"`
	OccurredAt string           `json:"occurred_at" format:"date-time" doc:"When it happened." example:"2026-10-31T09:00:00Z"`
	Actor      AuditActor       `json:"actor" doc:"Who acted."`
	OnBehalfOf *AuditActor      `json:"on_behalf_of,omitempty" doc:"The person an AI or a service acted for."`
	Originator string           `json:"originator,omitempty" doc:"The AI proposal the change carried out." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e55"`
	Action     string           `json:"action" doc:"What happened." example:"document.posted"`
	Record     AuditRecord      `json:"record" doc:"What it is about."`
	Reason     string           `json:"reason,omitempty" doc:"The reason given." example:"A supplier's October invoice arrived late"`
	Fields     []AuditFieldName `json:"fields" nullable:"false" doc:"The fields it changed; getAuditEvent gives their before and after."`
}

// AuditFieldChange is one field's before and after.
type AuditFieldChange struct {
	Field  string `json:"field" doc:"The field." example:"status"`
	Before any    `json:"before" doc:"Its value before; null when absent. A sensitive value is a proof, {fingerprint, hint}, never the value (AUD-02)." example:"\"draft\""`
	After  any    `json:"after" doc:"Its value after, the same way." example:"\"posted\""`
	Proof  string `json:"proof,omitempty" enum:"changed,changed_back,unchanged" doc:"For a sensitive value: whether it changed, changed back to what it was before the field's previous change, or not (AUD-02)." example:"changed"`
	Hint   string `json:"hint,omitempty" doc:"An identifier's last four characters." example:"******6789"`
	Sealed string `json:"sealed,omitempty" enum:"opened,shredded" doc:"For a person's personal value, sealed under their key for the event's financial year (§11.8): opened, before and after are the values; shredded, the key is gone at the end of retention and the values read null." example:"opened"`
}

// AuditRequest is where an event came from.
type AuditRequest struct {
	ID        string `json:"id,omitempty" doc:"The API request." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f99"`
	Session   string `json:"session,omitempty" doc:"The session." example:"s-1"`
	IP        string `json:"ip,omitempty" doc:"The IP address, while kept." example:"203.0.113.7"`
	UserAgent string `json:"user_agent,omitempty" doc:"The browser or client, while kept." example:"Mozilla/5.0 (Macintosh) Firefox/140.0"`
	Details   string `json:"details" enum:"in_body,kept,erased" doc:"in_body: kept in the event, as long as the records it is about; kept: a security event's, kept beside the chain until the security-log retention ends (AUD-06); erased: that retention ended." example:"kept"`
}

// AuditEventView is one event with its before and after values and its place in the chain.
type AuditEventView struct {
	AuditEventSummary
	ApprovalIDs []AuditApprovalID  `json:"approval_ids" nullable:"false" doc:"The approvals it carried out."`
	Request     AuditRequest       `json:"request" doc:"Where it came from."`
	Changes     []AuditFieldChange `json:"changes" nullable:"false" doc:"Each field's before and after."`
	PrevHash    string             `json:"prev_hash" doc:"The hash of the event before it, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Hash        string             `json:"hash" doc:"Its hash: SHA-256 of prev_hash and its body (INV-12)." example:"6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b"`
}

// AuditEventList is a page of the explorer's events, newest first.
type AuditEventList struct {
	Items      []AuditEventSummary `json:"items" nullable:"false" doc:"The events the filters match, newest first."`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjMwMCJ9"`
}

// AuditFilters are the explorer's filters, each optional.
type AuditFilters struct {
	Actor      string `query:"actor" json:"actor,omitempty" maxLength:"100" doc:"Only what this actor did: a user's, service's or integration's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	AI         string `query:"ai" json:"ai,omitempty" maxLength:"100" doc:"Only changes an AI proposal carried out: the proposal's ID, or any." example:"any"`
	RecordType string `query:"record_type" json:"record_type,omitempty" maxLength:"64" pattern:"^[a-z][a-z0-9_]*$" doc:"Only events about records of this type." example:"document"`
	RecordID   string `query:"record_id" json:"record_id,omitempty" maxLength:"36" doc:"Only events about this record." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Field      string `query:"field" json:"field,omitempty" maxLength:"100" doc:"Only events that changed this field." example:"status"`
	Action     string `query:"action" json:"action,omitempty" maxLength:"100" doc:"Only this action, or every action a prefix ending in . starts, such as document." example:"document."`
	ApprovalID string `query:"approval_id" json:"approval_id,omitempty" maxLength:"100" doc:"Only changes that carried out this approval." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
}

type auditEventsInput struct {
	PageQuery
	AuditFilters
	From string `query:"from" format:"date" doc:"Only events on or after this day, in the organisation's time zone." example:"2026-10-01"`
	To   string `query:"to" format:"date" doc:"Only events on or before this day." example:"2026-10-31"`
}

type auditEventsOutput struct{ Body AuditEventList }

type auditEventPath struct {
	EventID string `path:"event_id" format:"uuid" doc:"The event." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
}

type auditEventOutput struct{ Body AuditEventView }

// AuditTrailExportRequest asks for the audit trail's export.
type AuditTrailExportRequest struct {
	From    string       `json:"from" format:"date" doc:"The first day." example:"2026-10-01"`
	To      string       `json:"to" format:"date" doc:"The last day." example:"2026-10-31"`
	Format  string       `json:"format,omitempty" enum:"csv,xlsx" default:"xlsx" doc:"csv or xlsx. Left out, xlsx." example:"xlsx"`
	Filters AuditFilters `json:"filters,omitempty" doc:"The explorer's filters; left out, every event of the days."`
}

type auditTrailExportInput struct {
	KeyHeader
	PreferHeader
	Body AuditTrailExportRequest
}

// auditQuery is the filters as a query, refusing a malformed one.
func auditQuery(f AuditFilters, bad *[]FieldProblem, at string) storage.AuditQuery {
	q := storage.AuditQuery{Actor: f.Actor, Originator: f.AI, ObjectType: f.RecordType, Field: f.Field, Action: f.Action, ApprovalID: f.ApprovalID}
	if f.RecordID != "" {
		id, err := uuid.Parse(f.RecordID)
		if err != nil {
			*bad = append(*bad, FieldProblem{Code: CodeInvalid, Field: at + "record_id", Message: "is not an ID", Fix: "Send the record's ID"})
		}
		q.ObjectID = &id
	}
	return q
}

func auditSummaryOf(x audit.Entry) AuditEventSummary {
	out := AuditEventSummary{ID: x.ID.String(), Seq: x.Seq, OccurredAt: x.OccurredAt, Actor: AuditActor{Type: x.Actor.Type, ID: x.Actor.ID},
		Action: x.Action, Record: AuditRecord{Type: x.Object.Type, ID: x.Object.ID.String(), Version: x.Object.Version}, Reason: x.Reason,
		Fields: []AuditFieldName{}}
	for _, f := range x.Fields() {
		out.Fields = append(out.Fields, AuditFieldName(f))
	}
	if x.OnBehalfOf != nil {
		out.OnBehalfOf = &AuditActor{Type: x.OnBehalfOf.Type, ID: x.OnBehalfOf.ID}
	}
	if x.Originator != nil {
		out.Originator = *x.Originator
	}
	return out
}

func registerAuditEvents(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListAuditEvents, huma.Operation{
		OperationID: "listAuditEvents", Method: http.MethodGet, Path: "/v1/audit/events", Tags: []string{"Audit trail"},
		Summary: "List the audit trail's events",
		Description: "The audit trail explorer (FR-CTL-03): the organisation's events newest first, filtered by the actor, the AI proposal, the " +
			"record, a field, the action, an approval and the days, each filter optional. getAuditEvent gives one event's before and after " +
			"values. Read-only; no side effects.",
	}, append([]string{CodeInvalid, CodeCursor}, locks...), func(ctx context.Context, in *auditEventsInput) (*auditEventsOutput, error) {
		var bad []FieldProblem
		q := auditQuery(in.AuditFilters, &bad, "query.")
		var from, to time.Time
		var err error
		if in.From != "" {
			if from, err = time.Parse(time.DateOnly, in.From); err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.from", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
			}
		}
		if in.To != "" {
			if to, err = time.Parse(time.DateOnly, in.To); err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.to", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
			}
		}
		if after, err := decodeCursor(in.Cursor); err != nil {
			return nil, err
		} else if after != "" {
			if q.Before, err = strconv.ParseInt(after, 10, 64); err != nil {
				return nil, badCursor()
			}
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		q.Limit = in.PageSize
		if q.Limit == 0 {
			q.Limit = 100
		}
		p, _ := authn.From(ctx)
		out := auditEventsOutput{Body: AuditEventList{Items: []AuditEventSummary{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			loc, err := orgZone(ctx, u)
			if err != nil {
				return err
			}
			if in.From != "" {
				start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
				q.From = &start
			}
			if in.To != "" {
				end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
				q.To = &end
			}
			events, err := u.Audit().Find(ctx, q)
			if err != nil {
				return err
			}
			for _, e := range events {
				x, err := audit.Read(e)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, auditSummaryOf(x))
			}
			if len(events) == q.Limit {
				out.Body.NextCursor = encodeCursor(strconv.FormatInt(events[len(events)-1].Seq, 10))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetAuditEvent, huma.Operation{
		OperationID: "getAuditEvent", Method: http.MethodGet, Path: "/v1/audit/events/{event_id}", Tags: []string{"Audit trail"},
		Summary: "Get an audit event",
		Description: "One event of the audit trail (FR-CTL-03): who acted, on whose behalf, what, about which record, why, from where, and each " +
			"field's before and after. A sensitive value shows only whether it changed, or changed back (AUD-02); a security event's IP " +
			"address and browser while they are kept (AUD-06). Its place in the hash chain is given. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *auditEventPath) (*auditEventOutput, error) {
		id, err := parseID("event_id", in.EventID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out auditEventOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := u.Audit().Event(ctx, id)
			if err != nil {
				return err
			}
			x, err := audit.Read(e)
			if err != nil {
				return err
			}
			v := AuditEventView{AuditEventSummary: auditSummaryOf(x), ApprovalIDs: []AuditApprovalID{}, Changes: []AuditFieldChange{},
				PrevHash: hex.EncodeToString(x.PrevHash), Hash: hex.EncodeToString(x.Hash),
				Request: AuditRequest{ID: x.Request.ID, Session: x.Request.Session, IP: x.Request.IP, UserAgent: x.Request.UserAgent, Details: "in_body"}}
			for _, id := range x.ApprovalIDs {
				v.ApprovalIDs = append(v.ApprovalIDs, AuditApprovalID(id))
			}
			if x.Request.Detail != "" {
				d, err := u.Audit().Details(ctx, []uuid.UUID{x.ID})
				if err != nil {
					return err
				}
				if kept, ok := d[x.ID]; ok {
					v.Request.IP, v.Request.UserAgent, v.Request.Details = kept.IP, kept.UserAgent, "kept"
				} else {
					v.Request.Details = "erased"
				}
			}
			for _, c := range x.Changes {
				if c.Proof == audit.ProofChanged { // changed back? to the field's value before its previous change
					earlier, err := u.Audit().Find(ctx, storage.AuditQuery{ObjectType: x.Object.Type, ObjectID: &x.Object.ID, Field: c.Field,
						Before: x.Seq, Limit: 1})
					if err != nil {
						return err
					}
					if len(earlier) == 1 {
						if prev, err := audit.Read(earlier[0]); err == nil {
							for _, pc := range prev.Changes {
								if pc.Field == c.Field {
									c.ChangedBack(pc.Before)
								}
							}
						}
					}
				}
				v.Changes = append(v.Changes, AuditFieldChange{Field: c.Field, Before: c.Before, After: c.After, Proof: c.Proof, Hint: c.Hint})
			}
			if err := openSealed(ctx, u, x.ID, v.Changes); err != nil {
				return err
			}
			out.Body = v
			return nil
		})
		return &out, err
	})

	register(a, authz.RequestAuditTrailExport, huma.Operation{
		OperationID: "requestAuditTrailExport", Method: http.MethodPost, Path: "/v1/audit/exports", Tags: []string{"Audit trail"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for the audit trail's export",
		Description: "Asks for the events the explorer's filters match over a range of days as CSV or XLSX (FR-CTL-03, AUD-07), oldest first, " +
			"each as its body records it; a sensitive value stays a proof (AUD-02). The worker prepares it as a report export: the answer " +
			"is 202 with the export, pending, and its Location the operation to poll; getReportExport and getReportExportFile give it to " +
			"the caller only. Side effects: the export, its audit event (an export is a security event, AUD-05) and the job. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and asks for nothing.",
	}, []string{CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeReportExport},
		func(ctx context.Context, in *auditTrailExportInput) (*reportExportOutput, error) {
			p, _ := authn.From(ctx)
			b := in.Body
			var bad []FieldProblem
			auditQuery(b.Filters, &bad, "body.filters.")
			from, err1 := time.Parse(time.DateOnly, b.From)
			to, err2 := time.Parse(time.DateOnly, b.To)
			switch {
			case err1 != nil:
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.from", Message: "is not a date", Fix: "Send the first day as YYYY-MM-DD"})
			case err2 != nil:
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.to", Message: "is not a date", Fix: "Send the last day as YYYY-MM-DD"})
			case from.After(to):
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.from", Message: "is after to", Fix: "Send a first day on or before the last"})
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			f := b.Filters
			params, err := json.Marshal(reportexport.Parameters{From: b.From, To: b.To, Audit: &reportexport.AuditFilter{Actor: f.Actor,
				Originator: f.AI, ObjectType: f.RecordType, ObjectID: f.RecordID, Field: f.Field, Action: f.Action, ApprovalID: f.ApprovalID}})
			if err != nil {
				return nil, err
			}
			format := b.Format
			if format == "" {
				format = "xlsx"
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (ReportExportOut, error) {
				x, err := u.ReportExports().Ask(ctx, storage.NewReportExport{Report: reportexport.AuditTrail, Format: format, Parameters: params,
					RequestedBy: uploader(ctx), RequestedAt: a.deps.Clock.Now()})
				if err != nil {
					return ReportExportOut{}, err
				}
				var job storage.OutboxRow
				if a.deps.Outbox != nil {
					job, err = reportexport.Queue(ctx, *a.deps.Outbox, u, p.Organisation, x)
				} else { // no dispatcher here: the tick sends it
					var item storage.NewOutboxItem
					if item, err = reportexport.Item(x); err == nil {
						job, err = u.Outbox().Enqueue(ctx, item)
					}
				}
				if err != nil {
					return ReportExportOut{}, err
				}
				op, err := startOperation(ctx, u, "report_export", job.ID, "/v1/report-exports/"+x.ID.String(), a.deps.Clock.Now())
				out := reportExportOut(x)
				out.OperationID = op.ID.String()
				return out, err
			})
			if err != nil {
				return nil, err
			}
			out := &reportExportOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if w.Previewed {
				out.Body.OperationID = ""
			} else {
				out.Location = operationPath(w.Body.OperationID)
			}
			return out, nil
		})
}

// orgZone is the organisation's time zone, UTC when it has none.
func orgZone(ctx context.Context, u storage.UnitOfWork) (*time.Location, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		return time.UTC, nil
	}
	return loc, nil
}

// openSealed replaces each personal value sealed in changes with what it opens to (§11.8, rev 0.244), and marks the
// change opened, or shredded once the person's key is gone: the reader holds V on "Audit trail".
func openSealed(ctx context.Context, u storage.UnitOfWork, event uuid.UUID, changes []AuditFieldChange) error {
	var places []string // each sealed value's change, side and path, as "<change>/<side>/<path>"
	var values []storage.SealedValue
	place := func(i, side int, path string) string { return strconv.Itoa(i) + "/" + strconv.Itoa(side) + "/" + path }
	for i, c := range changes {
		for side, v := range []any{c.Before, c.After} {
			audit.Unseal(c.Field, v, func(path string, key uuid.UUID, text []byte) any {
				places = append(places, place(i, side, path))
				values = append(values, storage.SealedValue{EventID: event, Field: path, Key: key, Text: text})
				return nil
			})
		}
	}
	if len(values) == 0 {
		return nil
	}
	opened, err := u.Audit().OpenSealed(ctx, values)
	if err != nil {
		return err
	}
	at := make(map[string]storage.OpenedValue, len(opened))
	for n, o := range opened {
		at[places[n]] = o
	}
	for i := range changes {
		for side, v := range []*any{&changes[i].Before, &changes[i].After} {
			*v = audit.Unseal(changes[i].Field, *v, func(path string, _ uuid.UUID, _ []byte) any {
				o := at[place(i, side, path)]
				if o.Shredded {
					changes[i].Sealed = "shredded"
					return nil
				}
				if changes[i].Sealed == "" {
					changes[i].Sealed = "opened"
				}
				return o.Value
			})
		}
	}
	return nil
}
