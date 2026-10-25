package api

import (
	"context"
	"fmt"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankingest"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ mail received at the email-in addresses (FR-INT-22; rev 0.106)

// CodeSenderNotAddress: a sender that is not one email address.
const CodeSenderNotAddress = "EML-09"

// InboundFile is a received message's file, kept as untrusted data.
type InboundFile struct {
	Kind      string `json:"kind" enum:"body,attachment" doc:"The message's text, or an attachment." example:"attachment"`
	FileName  string `json:"file_name" doc:"Its name, as sent: data, never a path." example:"INV-0042.pdf"`
	MediaType string `json:"media_type" doc:"Its type, from its content." example:"application/pdf"`
	Size      int64  `json:"size_bytes" doc:"Its size." example:"48211"`
	Removed   bool   `json:"removed" doc:"true once its message was rejected or expired, and the file deleted." example:"false"`
}

// InboundMessage is a message received at an email-in address: never its content, which stays untrusted.
type InboundMessage struct {
	ID         string        `json:"id" format:"uuid" doc:"The message's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Address    string        `json:"address" doc:"The email-in address it came to." example:"bills@in.example.com"`
	For        string        `json:"for" enum:"organisation,member" doc:"Whose address: the organisation's, or a member's own." example:"organisation"`
	Kind       string        `json:"kind" enum:"documents,bank" doc:"The address's kind: documents captured, or the banks' reports at the bank-ingest address." example:"documents"`
	Sender     string        `json:"sender" doc:"Who sent it, as the receiving server saw it, in lower case." example:"accounts@supplier.example"`
	Subject    string        `json:"subject,omitempty" doc:"Its subject, as sent: untrusted." example:"Invoice INV-0042"`
	ReceivedAt time.Time     `json:"received_at" doc:"When it arrived." example:"2026-10-23T09:00:00Z"`
	Status     string        `json:"status" enum:"received,accepted,quarantined,rejected,expired,failed" doc:"received until it is fetched; accepted from a known or accepted sender; quarantined from anyone else until someone decides or its days pass; rejected from a rejected sender; expired when its days passed; failed when it could not be fetched." example:"quarantined"`
	SPF        string        `json:"spf,omitempty" doc:"The receiving server's SPF result." example:"pass"`
	DKIM       string        `json:"dkim,omitempty" doc:"Its DKIM result." example:"pass"`
	DMARC      string        `json:"dmarc,omitempty" doc:"Its DMARC result: a sender is known only when it passes, or when the sender's domain has no DMARC and SPF or DKIM passes." example:"pass"`
	ExpiresAt  *time.Time    `json:"expires_at,omitempty" doc:"When a quarantined message ends unless someone decides first; absent while it is kept until then." example:"2026-11-22T09:00:00Z"`
	Note       string        `json:"note,omitempty" doc:"What was not kept, or why it failed." example:"not kept: photo.heic: not a type Fyniq accepts"`
	Files      []InboundFile `json:"files" doc:"Its text and attachments, checked and scanned like an upload."`
}

// InboundMessagePage is a page of received messages, newest first.
type InboundMessagePage struct {
	Items      []InboundMessage `json:"items" nullable:"false" doc:"The messages, newest first."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// SenderDecision accepts or rejects a sender at an email-in address.
type SenderDecision struct {
	Sender   string `json:"sender" maxLength:"254" doc:"The sender's email address." example:"accounts@supplier.example"`
	Decision string `json:"decision" enum:"accepted,rejected" doc:"accepted: their mail is captured from now on, and their quarantined mail is accepted; rejected: their mail is refused, and their quarantined mail deleted." example:"accepted"`
}

// SenderDecisionResult is a decision and what it did to the quarantine.
type SenderDecisionResult struct {
	Sender   string `json:"sender" doc:"The sender, in lower case." example:"accounts@supplier.example"`
	Decision string `json:"decision" enum:"accepted,rejected" doc:"The decision now in force." example:"accepted"`
	Settled  int    `json:"settled" doc:"How many quarantined messages from them it accepted or rejected." example:"2"`
}

type inboundMessagesInput struct {
	PageQuery
	Status string `query:"status" enum:"received,accepted,quarantined,rejected,expired,failed" doc:"Only messages with this status." example:"quarantined"`
}

type senderDecisionInput struct {
	KeyHeader
	PreferHeader
	Body SenderDecision
}

type inboundMessagesOutput struct{ Body InboundMessagePage }

type senderDecisionOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     SenderDecisionResult
}

// inboundPage is a page of messages at address (nil: any of the organisation's), with their addresses' names.
func (a *API) inboundPage(ctx context.Context, u storage.UnitOfWork, in *inboundMessagesInput, address *uuid.UUID) (InboundMessagePage, error) {
	out := InboundMessagePage{Items: []InboundMessage{}}
	f := storage.InboundFilter{Status: in.Status, Address: address, Limit: in.PageSize + 1}
	if in.Cursor != "" {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return out, err
		}
		if f.Before, err = uuid.Parse(after); err != nil {
			return out, badCursor()
		}
	}
	ms, err := u.InboundMessages().List(ctx, f)
	if err != nil {
		return out, err
	}
	if len(ms) > in.PageSize {
		ms = ms[:in.PageSize]
		out.NextCursor = encodeCursor(ms[len(ms)-1].ID.String())
	}
	addresses := map[uuid.UUID]storage.CaptureAddress{}
	for _, m := range ms {
		addr, ok := addresses[m.AddressID]
		if !ok {
			if addr, err = u.CaptureAddresses().Get(ctx, m.AddressID); err != nil {
				return out, err
			}
			addresses[m.AddressID] = addr
		}
		files, err := u.InboundMessages().Files(ctx, m.ID)
		if err != nil {
			return out, err
		}
		v := InboundMessage{ID: m.ID.String(), Address: addr.LocalPart + "@" + a.deps.Mail.CaptureDomain, For: "organisation", Sender: m.Sender,
			Subject: m.Subject, ReceivedAt: m.ReceivedAt.UTC(), Status: m.Status, SPF: m.SPF, DKIM: m.DKIM, DMARC: m.DMARC, Note: m.LastError,
			Files: []InboundFile{}, Kind: addr.Kind}
		if addr.UserID != nil {
			v.For = "member"
		}
		if m.ExpiresAt != nil {
			at := m.ExpiresAt.UTC()
			v.ExpiresAt = &at
		}
		for _, x := range files {
			v.Files = append(v.Files, InboundFile{Kind: x.Kind, FileName: x.FileName, MediaType: x.MediaType, Size: x.Size, Removed: x.RemovedAt != nil})
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}

// decideSender records a decision about a sender at the address of kind of user (nil: the organisation's) and settles
// the sender's quarantined mail there; a rejection's files are deleted once the decision commits.
func (a *API) decideSender(ctx context.Context, in *senderDecisionInput, user *uuid.UUID, kind string) (*senderDecisionOutput, error) {
	parsed, err := netmail.ParseAddress(strings.TrimSpace(in.Body.Sender))
	if err != nil || parsed.Name != "" || !strings.Contains(parsed.Address, "@") {
		return nil, problem(CodeSenderNotAddress, fmt.Sprintf("%q is not one email address.", in.Body.Sender), "Give the sender's address alone, such as accounts@supplier.example",
			FieldProblem{CodeSenderNotAddress, "body.sender", "not an address", "Give one address"})
	}
	sender := strings.ToLower(parsed.Address)
	p, _ := authn.From(ctx)
	var deleted []string
	w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SenderDecisionResult, error) {
		deleted = nil
		if _, err := u.CaptureAddresses().Decide(ctx, sender, user, kind, in.Body.Decision); err != nil {
			return SenderDecisionResult{}, err
		}
		out := SenderDecisionResult{Sender: sender, Decision: in.Body.Decision}
		live, err := u.CaptureAddresses().Live(ctx)
		if err != nil {
			return out, err
		}
		addr := liveOf(live, user, kind)
		if addr == nil {
			return out, nil
		}
		ms, err := u.InboundMessages().QuarantinedFrom(ctx, sender, addr.ID)
		if err != nil {
			return out, err
		}
		for _, m := range ms {
			settled, keys, err := email.Settle(ctx, u, m.ID, in.Body.Decision)
			if err != nil {
				return out, err
			}
			if settled.Status == storage.InboundAccepted && a.deps.Outbox != nil { // an accepted bank report is imported (FR-BNK-17)
				if err := bankingest.Queue(ctx, *a.deps.Outbox, u, p.Organisation, settled, *addr); err != nil {
					return out, err
				}
			}
			deleted = append(deleted, keys...)
			out.Settled++
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	if !w.Previewed && !w.Replayed && a.deps.Staging != nil {
		for _, k := range deleted {
			if err := a.deps.Staging.Delete(context.WithoutCancel(ctx), k); err != nil {
				logFailure(ctx, err) // the row says it is removed; the blob stays until deleted by hand
			}
		}
	}
	return &senderDecisionOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}

func registerInbound(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	decides := append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSenderNotAddress}, locks...)

	register(a, authz.ListInboundMessages, huma.Operation{
		OperationID: "listInboundMessages", Method: http.MethodGet, Path: "/v1/organisation/email/inbound-messages", Tags: []string{"Organisation"},
		Summary: "List mail received at the email-in addresses",
		Description: "Mail received at the organisation's email-in addresses and its members', newest first, with the status the owner's rules " +
			"gave it (FR-INT-22): accepted from members and contacts, quarantined from anyone else until someone decides or its days pass, " +
			"rejected from rejected senders. Its subject is untrusted data, and its text and attachments are listed, never shown here. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *inboundMessagesInput) (*inboundMessagesOutput, error) {
		p, _ := authn.From(ctx)
		var out InboundMessagePage
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out, err = a.inboundPage(ctx, u, in, nil)
			return err
		})
		return &inboundMessagesOutput{Body: out}, err
	})

	register(a, authz.ListMyInboundMessages, huma.Operation{
		OperationID: "listMyInboundMessages", Method: http.MethodGet, Path: "/v1/organisation/email/inbound-messages/mine", Tags: []string{"Organisation"},
		Summary: "List mail received at your email-in address",
		Description: "Mail received at the caller's own email-in address, newest first (FR-INT-22): accepted from their own verified sign-in " +
			"email, quarantined from anyone else until they decide or its days pass. Empty while they have no address. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *inboundMessagesInput) (*inboundMessagesOutput, error) {
		p, _ := authn.From(ctx)
		out := InboundMessagePage{Items: []InboundMessage{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			live, err := u.CaptureAddresses().Live(ctx)
			if err != nil || p.UserID == uuid.Nil {
				return err
			}
			user := p.UserID
			mine := liveOf(live, &user, storage.CaptureDocuments)
			if mine == nil {
				return nil
			}
			out, err = a.inboundPage(ctx, u, in, &mine.ID)
			return err
		})
		return &inboundMessagesOutput{Body: out}, err
	})

	register(a, authz.DecideCaptureSender, huma.Operation{
		OperationID: "decideCaptureSender", Method: http.MethodPost, Path: "/v1/organisation/email/capture-senders", Tags: []string{"Organisation"},
		Summary: "Accept or reject a sender at the organisation's email-in address",
		Description: "Accepts or rejects a sender's mail at the organisation's email-in address (FR-INT-22; the owner's decisions of 23 Oct " +
			"2026), the latest decision in force: accepted mail is captured, provided its sender's domain vouches for it (DMARC, or SPF or " +
			"DKIM without DMARC); rejected mail is refused. The sender's quarantined mail there is accepted or rejected with it, and a " +
			"rejected message's files deleted. Side effects: the decision, each settled message and their audit events. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, decides, func(ctx context.Context, in *senderDecisionInput) (*senderDecisionOutput, error) {
		return a.decideSender(ctx, in, nil, storage.CaptureDocuments)
	})

	register(a, authz.DecideBankSender, huma.Operation{
		OperationID: "decideBankSender", Method: http.MethodPost, Path: "/v1/organisation/email/capture-senders/bank", Tags: []string{"Organisation"},
		Summary: "Accept or reject a sender's quarantined mail at the bank-ingest address",
		Description: "Accepts or rejects a sender's quarantined mail at the bank-ingest address (FR-BNK-17, FR-INT-22), as decideCaptureSender " +
			"does at the organisation's address. Accepting settles the mail waiting now; it does not make the sender known: only the senders " +
			"a money account registers for its emailed reports are, with DMARC passing. Rejecting refuses their mail there from now on. " +
			"Side effects: the decision, each settled message and their audit events. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, decides, func(ctx context.Context, in *senderDecisionInput) (*senderDecisionOutput, error) {
		return a.decideSender(ctx, in, nil, storage.CaptureBank)
	})

	register(a, authz.DecideMyCaptureSender, huma.Operation{
		OperationID: "decideMyCaptureSender", Method: http.MethodPost, Path: "/v1/organisation/email/capture-senders/mine", Tags: []string{"Organisation"},
		Summary: "Accept or reject a sender at your email-in address",
		Description: "Accepts or rejects a sender's mail at the caller's own email-in address (FR-INT-22), as decideCaptureSender does for the " +
			"organisation's, and settles their quarantined mail there. Refused for a caller who is not a person with a membership (EML-08). " +
			"Side effects: the decision, each settled message and their audit events. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append(decides, CodeCaptureNoUser), func(ctx context.Context, in *senderDecisionInput) (*senderDecisionOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeCaptureNoUser, "Only a person who is a member has an email-in address of their own.",
				"Sign in as yourself, or decide at the organisation's address")
		}
		user := p.UserID
		return a.decideSender(ctx, in, &user, storage.CaptureDocuments)
	})
}
