package reportexport

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// AuditTrail is the audit trail's export (FR-CTL-03, AUD-07; p1-audit-explorer design D6): the events the explorer's
// filters match over a range of days, oldest first, each as its body records it. A sensitive value stays a proof
// (AUD-02). It is asked for by requestAuditTrailExport, under Appendix D's "Audit trail", never by requestReportExport.
const AuditTrail = "audit_trail"

// AuditFilter is the explorer's filters an audit trail export keeps (storage.AuditQuery).
type AuditFilter struct {
	Actor      string `json:"actor,omitempty"`
	Originator string `json:"originator,omitempty"`
	ObjectType string `json:"object_type,omitempty"`
	ObjectID   string `json:"object_id,omitempty"`
	Field      string `json:"field,omitempty"`
	Action     string `json:"action,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

// MaxAuditRows is the most events one export holds.
const MaxAuditRows = 100000

// auditTrail is the export's one sheet: the events from the start of from to the end of to, in loc.
func auditTrail(ctx context.Context, u storage.UnitOfWork, f AuditFilter, from, to time.Time, loc *time.Location) ([]sheet.Sheet, error) {
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	q := storage.AuditQuery{Actor: f.Actor, Originator: f.Originator, ObjectType: f.ObjectType, Field: f.Field, Action: f.Action,
		ApprovalID: f.ApprovalID, From: &start, To: &end, Limit: 1000}
	if f.ObjectID != "" {
		id, err := uuid.Parse(f.ObjectID)
		if err != nil {
			return nil, Refusal{"its record is not an ID"}
		}
		q.ObjectID = &id
	}
	var events []storage.AuditEvent
	for {
		page, err := u.Audit().Find(ctx, q)
		if err != nil {
			return nil, err
		}
		events = append(events, page...)
		if len(events) > MaxAuditRows {
			return nil, Refusal{fmt.Sprintf("more than %d events match: export a shorter range, or narrow it", MaxAuditRows)}
		}
		if len(page) < q.Limit {
			break
		}
		q.Before = page[len(page)-1].Seq
	}
	slices.Reverse(events) // oldest first
	s := sheet.Sheet{Name: Names[AuditTrail], Header: []string{"Sequence", "Occurred at", "Actor", "On behalf of", "AI proposal", "Action",
		"Record type", "Record", "Version", "Reason", "Changes", "Approvals", "Request", "Hash"}}
	for _, e := range events {
		x, err := audit.Read(e)
		if err != nil {
			return nil, err
		}
		changes := map[string][2]any{}
		for _, c := range x.Changes {
			changes[c.Field] = [2]any{c.Before, c.After}
		}
		raw, err := json.Marshal(changes)
		if err != nil {
			return nil, err
		}
		on, ai, version := "", "", ""
		if x.OnBehalfOf != nil {
			on = x.OnBehalfOf.Type + ":" + x.OnBehalfOf.ID
		}
		if x.Originator != nil {
			ai = *x.Originator
		}
		if x.Object.Version != nil {
			version = strconv.Itoa(*x.Object.Version)
		}
		approvals, _ := json.Marshal(x.ApprovalIDs)
		s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Number(strconv.FormatInt(x.Seq, 10), 0), sheet.Text(x.OccurredAt),
			sheet.Text(x.Actor.Type + ":" + x.Actor.ID), sheet.Text(on), sheet.Text(ai), sheet.Text(x.Action), sheet.Text(x.Object.Type),
			sheet.Text(x.Object.ID.String()), sheet.Text(version), sheet.Text(x.Reason), sheet.Text(string(raw)), sheet.Text(string(approvals)),
			sheet.Text(x.Request.ID), sheet.Text(hex.EncodeToString(x.Hash))}})
	}
	return []sheet.Sheet{s}, nil
}
