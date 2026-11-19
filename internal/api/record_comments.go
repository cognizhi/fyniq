package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/comment"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ comments and @mentions (FR-UX-13, rev 0.376)
//
// A comment belongs to one record: a document or a contact. A member reads, writes and is mentioned on a record only if they may
// see it, which is the record's own row of Appendix D, as a search decides it (SEC-34), and for an Approver narrowed to departments
// their departments' records only (FR-ADM-01). A record the caller may not see is not found, so nothing of it is learnt, and the
// names offered to mention are those of members who may open it: a name of someone who may not is never offered, and a mention of
// one is refused as though they were not a member.

// commentKinds are the records that take comments and the row of Appendix D that decides who sees each: the kinds a search finds.
var commentKinds = func() map[string]authz.Capability {
	out := map[string]authz.Capability{"contact": authz.ContactsAndItems}
	for _, g := range searchGroups {
		for _, t := range g.Types {
			out[string(t)] = g.Row
		}
	}
	return out
}()

// commentLabels name a kind of record in a notification.
var commentLabels = map[string]string{"invoice": "invoice", "credit_note": "credit note", "bill": "bill", "vendor_credit": "vendor credit", "journal": "journal",
	"expense_claim": "expense claim", "quote": "quote", "sales_order": "sales order", "purchase_order": "purchase order", "contact": "contact"}

// commentRecord is a record a comment is on: what decides who may see it, and what to call it.
type commentRecord struct {
	Type     string
	ID       uuid.UUID
	Row      authz.Capability
	Claimant uuid.UUID   // an expense claim's claimant
	Values   []uuid.UUID // the dimension values on the lines of a bill, an invoice or a claim
	Title    string
	Link     string
}

// CodeCommentRecord refuses a record that takes no comments.
const CodeCommentRecord = comment.CodeRecord

// commentRecordOf loads the record a comment is on, or ports.ErrNotFound: no such record, or a record of another type.
func commentRecordOf(ctx context.Context, u storage.UnitOfWork, recordType string, id uuid.UUID) (commentRecord, error) {
	row, ok := commentKinds[recordType]
	if !ok {
		return commentRecord{}, problem(CodeCommentRecord, fmt.Sprintf("%q is not a kind of record that takes comments.", recordType),
			"Comment on an invoice, credit note, bill, vendor credit, journal, expense claim, quote, sales order, purchase order or contact")
	}
	rec := commentRecord{Type: recordType, ID: id, Row: row}
	if recordType == "contact" {
		c, err := u.Contacts().Get(ctx, id)
		if err != nil {
			return rec, err
		}
		rec.Title, rec.Link = "contact "+c.LegalName, "/contacts/"+id.String()
		return rec, nil
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return rec, err
	}
	if string(doc.Type) != recordType {
		return rec, ports.ErrNotFound
	}
	label := commentLabels[recordType]
	rec.Title, rec.Link = cmpOr(doc.Number, "a draft "+label), linkOf(doc.Type, id.String(), false)
	if doc.Number != "" {
		rec.Title = label + " " + doc.Number
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return rec, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return rec, err
	}
	switch doc.Type {
	case storage.DocumentBill, storage.DocumentVendorCredit:
		d, err := bill.Read(doc, base)
		if err != nil {
			return rec, err
		}
		for _, l := range d.Lines {
			rec.Values = append(rec.Values, l.Values...)
		}
	case storage.DocumentInvoice, storage.DocumentCreditNote:
		d, err := invoice.Read(doc, base)
		if err != nil {
			return rec, err
		}
		for _, l := range d.Lines {
			rec.Values = append(rec.Values, l.Values...)
		}
	case storage.DocumentExpenseClaim:
		d, err := claim.Read(doc, base)
		if err != nil {
			return rec, err
		}
		rec.Claimant = d.ClaimantID
		for _, l := range d.Lines {
			rec.Values = append(rec.Values, l.Values...)
		}
	}
	return rec, nil
}

// seenBy reports whether p may see the record: the right to view its row without a qualifier, the Approver's limit-qualified right on
// the rows of the requests they decide (bills, invoices and expense claims), or, for an expense claim, the Employee's own claim; and
// for an Approver narrowed to departments, a record in their departments (their own claims aside).
func (rec commentRecord) seenBy(p authn.Principal, departments []uuid.UUID) bool {
	view := authz.Policy{Operation: "comment", Capability: rec.Row, Need: authz.View}
	unqualified := view
	unqualified.Unqualified = true
	approves := rec.Row == authz.Bills || rec.Row == authz.Invoices || rec.Row == authz.ExpenseClaims
	own := rec.Type == "expense_claim" && p.UserID != uuid.Nil && rec.Claimant == p.UserID && authz.Decide(view, p.Roles, p.AI()).Allowed
	byApprover := approves && slices.Contains(p.Roles, authz.Approver) && authz.Decide(view, []authz.Role{authz.Approver}, p.AI()).Allowed
	if !own && !byApprover && !authz.Decide(unqualified, p.Roles, p.AI()).Allowed {
		return false
	}
	if own || !approves || !narrowedTo(p, rec.Row, authz.View) {
		return true
	}
	return (&departmentReach{scope: p.Approver, departments: departments}).holds(rec.Values)
}

// memberAs is a member as the principal their roles make, with the department scope of their Approver role.
func memberAs(m storage.Member) authn.Principal {
	roles := make([]authz.Role, 0, len(m.Roles))
	for _, r := range m.Roles {
		roles = append(roles, authz.Role(r.Role))
	}
	return authn.Principal{UserID: m.UserID, Class: authn.ClassUser, Roles: roles, Approver: approverScopeOf(m.Roles)}
}

// commentAccess loads the record and says whether the caller may see it; not found for a record they may not.
func commentAccess(ctx context.Context, u storage.UnitOfWork, p authn.Principal, recordType string, id uuid.UUID) (commentRecord, []uuid.UUID, error) {
	rec, err := commentRecordOf(ctx, u, recordType, id)
	if err != nil {
		return rec, nil, err
	}
	departments, err := departmentValues(ctx, u)
	if err != nil {
		return rec, nil, err
	}
	if !rec.seenBy(p, departments) {
		return rec, nil, ports.ErrNotFound
	}
	return rec, departments, nil
}

// RecordComment is a comment on a record.
type RecordComment struct {
	ID         string    `json:"id" format:"uuid" doc:"The comment's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a01"`
	RecordType string    `json:"record_type" doc:"The kind of record it is on." example:"bill"`
	RecordID   string    `json:"record_id" format:"uuid" doc:"The record it is on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Author     Person    `json:"author" doc:"Who wrote it."`
	Body       string    `json:"body" doc:"What they wrote, as plain text with line breaks." example:"The GST on this line looks high: please check the supplier's invoice."`
	Mentions   []Person  `json:"mentions" nullable:"false" doc:"The members it mentions: each could see the record when it was written, and was told."`
	CreatedAt  time.Time `json:"created_at" doc:"When it was written." example:"2026-11-20T09:30:00Z"`
}

// RecordCommentPage is a page of a record's comments, oldest first.
type RecordCommentPage struct {
	Items      []RecordComment `json:"items" nullable:"false" doc:"The record's comments in this page, oldest first."`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// Mentionable is a member who may be mentioned on a record: one who may see it.
type Mentionable struct {
	ID   string `json:"id" format:"uuid" doc:"The member's user ID, to send as a mention." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Name string `json:"name" doc:"Their name." example:"Wei Ling Tan"`
}

// MentionablePage is the members who may be mentioned on a record.
type MentionablePage struct {
	Items      []Mentionable `json:"items" nullable:"false" doc:"The members who may see the record, by name; the caller is left out."`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// RecordCommentPath names a record a comment is on.
type RecordCommentPath struct {
	RecordType string `path:"record_type" enum:"invoice,credit_note,bill,vendor_credit,journal,expense_claim,quote,sales_order,purchase_order,contact" doc:"The kind of record." example:"bill"`
	RecordID   string `path:"record_id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
}

// NewRecordComment is what to write.
type NewRecordComment struct {
	Body       string   `json:"body" minLength:"1" maxLength:"2000" doc:"The comment, as plain text; 2,000 characters at most." example:"@Wei Ling the GST on this line looks high: please check the supplier's invoice."`
	MentionIDs []string `json:"mention_ids,omitempty" maxItems:"10" doc:"The members to mention, by user ID (listRecordMentionable): each must be able to see the record, or the comment is refused. They are told."`
}

// TransformSchema implements huma.SchemaTransformer: an example for each mention (NFR-API-01).
func (NewRecordComment) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"mention_ids": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"})
	return s
}

type listRecordCommentsInput struct {
	RecordCommentPath
	PageQuery
}

type addRecordCommentInput struct {
	RecordCommentPath
	KeyHeader
	PreferHeader
	Body NewRecordComment
}

type recordCommentOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     RecordComment
}

type recordCommentsOutput struct{ Body RecordCommentPage }

type listMentionableInput struct {
	RecordCommentPath
	PageQuery
	Q string `query:"q" maxLength:"100" doc:"Only members whose name starts with, or has a word that starts with, this; every one who may see the record when left out." example:"wei"`
}

type mentionableOutput struct{ Body MentionablePage }

// recordMembers are the active members who may see the record, by name, with their names by user ID for the comments' authors.
func recordMembers(ctx context.Context, u storage.UnitOfWork, now time.Time, rec commentRecord, departments []uuid.UUID) (names map[uuid.UUID]string, may []storage.Member, err error) {
	ms, err := u.Members().List(ctx, now)
	if err != nil {
		return nil, nil, err
	}
	names = map[uuid.UUID]string{}
	for _, m := range ms {
		names[m.UserID] = m.DisplayName
		if m.Status == "active" && rec.seenBy(memberAs(m), departments) {
			may = append(may, m)
		}
	}
	return names, may, nil
}

func commentOut(c storage.RecordComment, names map[uuid.UUID]string) RecordComment {
	out := RecordComment{ID: c.ID.String(), RecordType: c.RecordType, RecordID: c.RecordID.String(), Author: Person{ID: c.Author.String(), Name: names[c.Author]},
		Body: c.Body, Mentions: []Person{}, CreatedAt: c.CreatedAt.UTC()}
	for _, m := range c.Mentions {
		out.Mentions = append(out.Mentions, Person{ID: m.String(), Name: names[m]})
	}
	return out
}

func registerRecordComments(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListRecordComments, huma.Operation{
		OperationID: "listRecordComments", Method: http.MethodGet, Path: "/v1/records/{record_type}/{record_id}/comments", Tags: []string{"Records"},
		Summary: "List a record's comments",
		Description: "The comments on an invoice, credit note, bill, vendor credit, journal, expense claim, quote, sales order, purchase order or contact, " +
			"oldest first, a page at a time (FR-UX-13). Whoever may see the record reads them; for a record the caller may not see, the answer is " +
			"404, as it is for one that does not exist (SEC-34). Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeCommentRecord, CodeCursor}, locks...), func(ctx context.Context, in *listRecordCommentsInput) (*recordCommentsOutput, error) {
		id, err := parseID("record_id", in.RecordID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var all []RecordComment
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rec, departments, err := commentAccess(ctx, u, p, in.RecordType, id)
			if err != nil {
				return err
			}
			thread, err := u.RecordComments().Thread(ctx, in.RecordType, id)
			if err != nil {
				return err
			}
			names, _, err := recordMembers(ctx, u, a.deps.Clock.Now(), rec, departments)
			if err != nil {
				return err
			}
			for _, c := range thread {
				all = append(all, commentOut(c, names))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(c RecordComment) string { return c.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []RecordComment{}
		}
		return &recordCommentsOutput{Body: RecordCommentPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.ListRecordMentionable, huma.Operation{
		OperationID: "listRecordMentionable", Method: http.MethodGet, Path: "/v1/records/{record_type}/{record_id}/mentionable", Tags: []string{"Records"},
		Summary: "List the members who may be mentioned on a record",
		Description: "The active members who may see the record, by name (FR-UX-13), the caller left out, a page at a time; q keeps those whose name, or a " +
			"word of it, starts with it. Only members who may open the record are listed, so a name is never offered for a record its owner may not see; " +
			"for a record the caller may not see, the answer is 404. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeCommentRecord, CodeCursor}, locks...), func(ctx context.Context, in *listMentionableInput) (*mentionableOutput, error) {
		id, err := parseID("record_id", in.RecordID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var all []Mentionable
		q := strings.ToLower(strings.TrimSpace(in.Q))
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rec, departments, err := commentAccess(ctx, u, p, in.RecordType, id)
			if err != nil {
				return err
			}
			_, may, err := recordMembers(ctx, u, a.deps.Clock.Now(), rec, departments)
			if err != nil {
				return err
			}
			for _, m := range may {
				if m.UserID == p.UserID || (q != "" && !nameStarts(m.DisplayName, q)) {
					continue
				}
				all = append(all, Mentionable{ID: m.UserID.String(), Name: m.DisplayName})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		slices.SortFunc(all, func(x, y Mentionable) int { return strings.Compare(strings.ToLower(x.Name)+x.ID, strings.ToLower(y.Name)+y.ID) })
		items, next, err := pageOf(all, func(m Mentionable) string { return m.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []Mentionable{}
		}
		return &mentionableOutput{Body: MentionablePage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.AddRecordComment, huma.Operation{
		OperationID: "addRecordComment", Method: http.MethodPost, Path: "/v1/records/{record_type}/{record_id}/comments", Tags: []string{"Records"},
		DefaultStatus: http.StatusCreated, Summary: "Comment on a record",
		Description: "Adds a comment, as the caller, to the record's thread, and mentions members by user ID (FR-UX-13). Whoever may see the record comments; each " +
			"person mentioned must be able to see it too, or the comment is refused, as though they were not a member, and only those mentioned are told. A comment " +
			"is never changed or removed: answer a mistake with another comment. Side effects: the comment, its audit event and a notification to each person " +
			"mentioned. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeCommentRecord, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		comment.CodeBody, comment.CodeMentions, comment.CodeNoUser}, locks...), func(ctx context.Context, in *addRecordCommentInput) (*recordCommentOutput, error) {
		id, err := parseID("record_id", in.RecordID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser || p.AI() {
			return nil, problem(comment.CodeNoUser, "Only a person who is a member comments.", "Sign in as yourself")
		}
		body, bad := comment.Clean(in.Body.Body)
		if bad == nil {
			bad = comment.CheckMentions(in.Body.MentionIDs)
		}
		if bad != nil {
			return nil, invalid([]FieldProblem{{Code: bad.Code, Field: "body." + bad.Field, Message: bad.Message, Fix: bad.Fix}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RecordComment, error) {
			rec, departments, err := commentAccess(ctx, u, p, in.RecordType, id)
			if err != nil {
				return RecordComment{}, err
			}
			now := a.deps.Clock.Now()
			names, may, err := recordMembers(ctx, u, now, rec, departments)
			if err != nil {
				return RecordComment{}, err
			}
			var mentions []uuid.UUID
			var fields []FieldProblem
			for i, raw := range in.Body.MentionIDs {
				who, err := uuid.Parse(raw)
				if err != nil || !slices.ContainsFunc(may, func(m storage.Member) bool { return m.UserID == who }) {
					// The same words for someone who does not exist, is not a member, or may not open the record: nothing is learnt.
					fields = append(fields, FieldProblem{Code: comment.CodeMentions, Field: fmt.Sprintf("body.mention_ids[%d]", i),
						Message: "is not a member who may see this record", Fix: "Mention a member from the list of those who may see it (listRecordMentionable)"})
					continue
				}
				mentions = append(mentions, who)
			}
			if len(fields) > 0 {
				return RecordComment{}, invalid(fields)
			}
			c, err := u.RecordComments().Add(ctx, storage.NewRecordComment{RecordType: in.RecordType, RecordID: id, Author: p.UserID, Body: body, Mentions: mentions, CreatedAt: now})
			if err != nil {
				return RecordComment{}, err
			}
			if told := slices.DeleteFunc(slices.Clone(mentions), func(m uuid.UUID) bool { return m == p.UserID }); len(told) > 0 {
				if err := a.notify(ctx, u, notify.Event{Type: notify.CommentMentionType, RecordType: rec.Type, RecordID: &rec.ID,
					Title: cmpOr(names[p.UserID], "A member") + " mentioned you on " + rec.Title, Link: rec.Link, At: now,
					Audience: notify.Audience{Users: told}}); err != nil {
					return RecordComment{}, err
				}
			}
			return commentOut(c, names), nil
		})
		if err != nil {
			return nil, err
		}
		return &recordCommentOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// nameStarts reports whether name, or a word of it, starts with q (lower case).
func nameStarts(name, q string) bool {
	for _, w := range strings.Fields(strings.ToLower(name)) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}
