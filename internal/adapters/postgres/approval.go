package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Approvals() storage.Approvals { return approvals{u} }

type approvals struct{ u *unitOfWork }

// approvalRow is every query's request row: they all select the same columns.
type approvalRow = sqlcdb.GetApprovalRequestRow

func toApproval(r approvalRow) storage.ApprovalRequest {
	return storage.ApprovalRequest{ID: r.ID, ObjectType: r.ObjectType, ObjectKey: r.ObjectKey, Operation: r.Operation, Summary: r.Summary,
		Change: r.Change, Envelope: r.Envelope, EnvelopeHash: r.EnvelopeHash, PolicyVersion: r.PolicyVersion, RequestedBy: r.RequestedBy,
		RequestedAt: r.RequestedAt, RequesterMFAMethod: r.RequesterMfaMethod, RequesterMFAAt: r.RequesterMfaAt, ExpiresAt: r.ExpiresAt,
		Status: r.Status, DecidedAt: r.DecidedAt, StatusReason: str(r.StatusReason), Version: int(r.Version)}
}

func toSignature(r sqlcdb.ApprovalSignaturesRow) storage.ApprovalSignature {
	return storage.ApprovalSignature{ID: r.ID, RequestID: r.RequestID, Step: int(r.Step), Signer: r.Signer, Decision: r.Decision,
		EnvelopeHash: r.EnvelopeHash, MFAMethod: r.MfaMethod, MFAAt: r.MfaAt, SignedAt: r.SignedAt, Reason: str(r.Reason), VoidedAt: r.VoidedAt}
}

func (r approvals) Request(ctx context.Context, n storage.NewApprovalRequest) (storage.ApprovalRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.ApprovalRequest{}, err
	}
	row, err := r.u.q.InsertApprovalRequest(ctx, sqlcdb.InsertApprovalRequestParams{ObjectType: n.ObjectType, ObjectKey: n.ObjectKey,
		Operation: n.Operation, Summary: n.Summary, Change: n.Change, Envelope: n.Envelope, EnvelopeHash: n.EnvelopeHash,
		PolicyVersion: n.PolicyVersion, RequestedBy: n.RequestedBy, RequestedAt: n.RequestedAt, RequesterMfaMethod: n.RequesterMFAMethod,
		RequesterMfaAt: n.RequesterMFAAt, ExpiresAt: n.ExpiresAt})
	if err != nil {
		return storage.ApprovalRequest{}, mapError(err)
	}
	a := toApproval(approvalRow(row))
	if err := r.u.event(ctx, "approval.requested", "approval_request", a.ID, &a.Version, map[string][2]any{
		"object_type": {nil, a.ObjectType}, "object_key": {nil, a.ObjectKey}, "operation": {nil, a.Operation}, "summary": {nil, a.Summary},
		"envelope_hash": {nil, hex.EncodeToString(a.EnvelopeHash)}, "policy_version": {nil, a.PolicyVersion},
		"requested_by": {nil, a.RequestedBy}, "requester_mfa_method": {nil, a.RequesterMFAMethod}, "expires_at": {nil, a.ExpiresAt}}); err != nil {
		return storage.ApprovalRequest{}, err
	}
	return a, nil
}

func (r approvals) Get(ctx context.Context, id uuid.UUID) (storage.ApprovalRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.ApprovalRequest{}, err
	}
	row, err := r.u.q.GetApprovalRequest(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ApprovalRequest{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ApprovalRequest{}, mapError(err)
	}
	return toApproval(row), nil
}

func (r approvals) Lock(ctx context.Context, id uuid.UUID) (storage.ApprovalRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.ApprovalRequest{}, err
	}
	row, err := r.u.q.LockApprovalRequest(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ApprovalRequest{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ApprovalRequest{}, mapError(err)
	}
	return toApproval(approvalRow(row)), nil
}

func (r approvals) List(ctx context.Context, status string) ([]storage.ApprovalRequest, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListApprovalRequests(ctx, status)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ApprovalRequest, len(rows))
	for i, row := range rows {
		out[i] = toApproval(approvalRow(row))
	}
	return out, nil
}

func (r approvals) Signatures(ctx context.Context, requestID uuid.UUID) ([]storage.ApprovalSignature, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ApprovalSignatures(ctx, requestID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ApprovalSignature, len(rows))
	for i, row := range rows {
		out[i] = toSignature(row)
	}
	return out, nil
}

func (r approvals) Sign(ctx context.Context, s storage.NewApprovalSignature) (storage.ApprovalSignature, error) {
	if err := r.u.check(); err != nil {
		return storage.ApprovalSignature{}, err
	}
	if s.Step < 1 || s.Step > 20 {
		return storage.ApprovalSignature{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("step %d of an approval", s.Step)}
	}
	row, err := r.u.q.InsertApprovalSignature(ctx, sqlcdb.InsertApprovalSignatureParams{RequestID: s.RequestID, Step: int32(s.Step), // 1 to 20, checked above
		Signer: s.Signer, Decision: s.Decision, EnvelopeHash: s.EnvelopeHash, MfaMethod: s.MFAMethod, MfaAt: s.MFAAt, SignedAt: s.SignedAt,
		Reason: opt(s.Reason)})
	if err != nil {
		return storage.ApprovalSignature{}, mapError(err)
	}
	sig := toSignature(sqlcdb.ApprovalSignaturesRow(row))
	action := "approval.given"
	if sig.Decision == "reject" {
		action = "approval.refused"
	}
	if err := r.u.event(ctx, action, "approval_request", sig.RequestID, nil, map[string][2]any{
		"signature_id": {nil, sig.ID}, "step": {nil, sig.Step}, "signer": {nil, sig.Signer}, "decision": {nil, sig.Decision},
		"envelope_hash": {nil, hex.EncodeToString(sig.EnvelopeHash)}, "mfa_method": {nil, sig.MFAMethod},
		"mfa_at": {nil, sig.MFAAt}, "signed_at": {nil, sig.SignedAt}, "reason": {nil, nilIfEmpty(sig.Reason)}}); err != nil {
		return storage.ApprovalSignature{}, err
	}
	return sig, nil
}

func (r approvals) Decide(ctx context.Context, id uuid.UUID, version int, status, reason string, at time.Time) (storage.ApprovalRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.ApprovalRequest{}, err
	}
	if version < 1 || version > 1_000_000 {
		return storage.ApprovalRequest{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("version %d of an approval request", version)}
	}
	row, err := r.u.q.DecideApprovalRequest(ctx, sqlcdb.DecideApprovalRequestParams{Status: status, DecidedAt: &at, Reason: opt(reason), ID: id,
		Version: int32(version)}) // 1 to 1,000,000, checked above
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ApprovalRequest{}, &storage.Error{Code: storage.CodeVersion,
			Message: "the approval request was decided or changed since it was read"}
	}
	if err != nil {
		return storage.ApprovalRequest{}, mapError(err)
	}
	a := toApproval(approvalRow(row))
	if err := r.u.event(ctx, "approval."+a.Status, "approval_request", a.ID, &a.Version, map[string][2]any{
		"status": {"pending", a.Status}, "decided_at": {nil, a.DecidedAt}, "status_reason": {nil, nilIfEmpty(a.StatusReason)}}); err != nil {
		return storage.ApprovalRequest{}, err
	}
	return a, nil
}

// auditVoids records an audit event for each pending request the database voided in this transaction because
// its record changed (migration 000030): the change that voided it and the void commit together (AUD-01).
func (u *unitOfWork) auditVoids(ctx context.Context) error {
	rows, err := u.q.VoidedApprovalRequests(ctx)
	if err != nil {
		return mapError(err)
	}
	for _, row := range rows {
		if u.voided == nil {
			u.voided = map[uuid.UUID]bool{}
		}
		if u.voided[row.ID] {
			continue
		}
		u.voided[row.ID] = true
		version := int(row.Version)
		if err := u.event(ctx, "approval.void", "approval_request", row.ID, &version, map[string][2]any{
			"status": {"pending", "void"}, "status_reason": {nil, str(row.StatusReason)}}); err != nil {
			return err
		}
	}
	return nil
}
