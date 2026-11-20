package compliance

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MCPActorPrefix starts the audit actor of every change an AI app's delegated token makes over MCP (AI-16, CTL-MCP-01): the API
// records it as "mcp:" and the client the token was issued to.
const MCPActorPrefix = "mcp:"

// auditWindow is the audit events since a time, newest first, read in pages: at most 20,000 (a day of an organisation's trail is
// far less; a longer one is read by the next night's run from where this one stopped being checked, so it is bounded, not skipped).
func auditSince(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]audit.Entry, error) {
	var out []audit.Entry
	var before int64
	for range 20 {
		page, err := u.Audit().Find(ctx, storage.AuditQuery{From: &since, Before: before, Limit: 1000})
		if err != nil {
			return nil, err
		}
		for _, e := range page {
			entry, err := audit.Read(e)
			if err != nil {
				return nil, err
			}
			out = append(out, entry)
		}
		if len(page) < 1000 {
			break
		}
		before = page[len(page)-1].Seq
	}
	return out, nil
}

// mcpAgentWrites (CTL-MCP-01, AI-34, AI-16; p1-mcp-first-party task 10.2): of the day's audit events, every one made by an AI app's
// delegated token is a proposal's or its approval request's (the one write an agent-class token has), and nothing else; the sample is every such event, not a
// sample of them (target 0).
func mcpAgentWrites(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	entries, err := auditSince(ctx, u, since)
	if err != nil {
		return nil, "", err
	}
	found, n := mcpWriteFindings(entries)
	return found, fmt.Sprintf("%d change(s) made by AI apps' tokens", n), nil
}

// mcpWriteFindings are the events an AI app's token made that are not an AI proposal's, and how many it made.
func mcpWriteFindings(entries []audit.Entry) ([]Finding, int) {
	var out []Finding
	n := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Actor.ID, MCPActorPrefix) {
			continue
		}
		n++
		// The proposal's own approval request is asked as the person, by the app's delegation (requester method mcp_token): part of
		// the proposal, never of another change.
		request := e.Action == "approval.requested" && changed(e, "object_type") == "ai_proposal" && changed(e, "requester_mfa_method") == "mcp_token"
		if !strings.HasPrefix(e.Action, "ai_proposal.") && !request {
			out = append(out, Finding{Kind: "audit_event", ID: e.ID.String(), At: day(parseTime(e.OccurredAt)),
				Detail: "An AI app's token made " + e.Action + ": its only write is a proposal"})
		}
	}
	return out, n
}

// mcpSettings are the settings that open MCP, each of which changes only with a second approver (AI-35, AI-90).
var mcpSettings = []string{"features.mcp", "mcp.clients", "mcp.registration", "mcp.third_party"}

// mcpEnablement (CTL-MCP-03, AI-35): of the day's changes of the settings that open MCP, each that opens it (the switch or the
// registration on, an app added to the allowlist) was carried out by an approval that is approved and signed by someone other than
// whoever asked for it (target 0). Turning one off, or taking an app off, needs none.
func mcpEnablement(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	entries, err := auditSince(ctx, u, since)
	if err != nil {
		return nil, "", err
	}
	var out []Finding
	n := 0
	for _, e := range entries {
		if e.Action != "organisation_setting.changed" || !opens(e) {
			continue
		}
		n++
		ok := false
		for _, id := range e.ApprovalIDs {
			aid, err := uuid.Parse(id)
			if err != nil {
				continue
			}
			r, err := u.Approvals().Get(ctx, aid)
			if err != nil || r.Status != "approved" {
				continue
			}
			sigs, err := u.Approvals().Signatures(ctx, aid)
			if err != nil {
				return nil, "", err
			}
			if slices.ContainsFunc(sigs, func(s storage.ApprovalSignature) bool {
				return s.Decision == "approve" && s.VoidedAt == nil && s.Signer != r.RequestedBy
			}) {
				ok = true
			}
		}
		if !ok {
			out = append(out, Finding{Kind: "organisation_setting", ID: e.ID.String(), At: day(parseTime(e.OccurredAt)),
				Detail: "It opened MCP (" + changed(e, "key") + ") without an approval signed by a second person"})
		}
	}
	return out, fmt.Sprintf("%d change(s) that opened MCP", n), nil
}

func changed(e audit.Entry, field string) string {
	for _, c := range e.Changes {
		if c.Field == field {
			return fmt.Sprint(c.After)
		}
	}
	return ""
}

// opens reports whether a change of a setting opens MCP: the switch or the registration turned on, or an app added to the allowlist.
func opens(e audit.Entry) bool {
	key := changed(e, "key")
	if !slices.Contains(mcpSettings, key) {
		return false
	}
	var was any
	for _, c := range e.Changes {
		if c.Field == "value" {
			was = c.Before
		}
	}
	value := changed(e, "value")
	if key == "mcp.clients" {
		before := map[string]bool{}
		for _, a := range strings.Split(fmt.Sprint(was), ",") {
			before[strings.TrimSpace(a)] = true
		}
		for _, a := range strings.Split(value, ",") {
			if a = strings.TrimSpace(a); a != "" && !before[a] {
				return true
			}
		}
		return false
	}
	return value == "true"
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// integrationsOwned (CTL-API-02, SEC-07; p1-mcp-first-party task 10.2) is the nightly look at the organisation's integration clients as
// Fyniq holds them: each one that is not revoked has an owner who is an active member, a class of integration or agent, an end date that
// had not passed when the day's window began, and only its class's scopes (an agent holds read and propose scopes, never a write; an integration client reads and
// writes by area). Each exception is listed by client (target 0).
func integrationsOwned(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	clients, err := u.APIClients().List(ctx, storage.APIClientIntegration)
	if err != nil {
		return nil, "", err
	}
	members, err := u.Members().List(ctx, since)
	if err != nil {
		return nil, "", err
	}
	active := map[uuid.UUID]bool{}
	for _, m := range members {
		active[m.UserID] = true
	}
	out, live := integrationFindings(clients, active, since)
	return out, fmt.Sprintf("%d live integration client(s)", live), nil
}

// integrationFindings are the live integration clients that break CTL-API-02, and how many were live.
func integrationFindings(clients []storage.APIClient, active map[uuid.UUID]bool, since time.Time) ([]Finding, int) {
	var out []Finding
	live := 0
	for _, c := range clients {
		if c.RevokedAt != nil {
			continue
		}
		live++
		add := func(why string) { out = append(out, Finding{Kind: "integration_client", ID: c.ClientID, Detail: why}) }
		switch {
		case c.OwnerID == uuid.Nil || !active[c.OwnerID]:
			add("It has no owner who is an active member")
		case c.Class != "integration" && c.Class != "agent":
			add("Its class is neither integration nor agent")
		}
		if c.ExpiresAt.IsZero() {
			add("It has no end date")
		} else if c.ExpiresAt.Before(since) {
			add("Its end date passed and it was not revoked")
		}
		for _, s := range c.Scopes {
			ok := strings.HasPrefix(s, "fyniq.read.") || (c.Class == "integration" && strings.HasPrefix(s, "fyniq.write."))
			if !ok && s != "openid" && s != "offline_access" {
				add("It holds the scope " + s + ", which its class does not")
			}
		}
	}
	return out, live
}
