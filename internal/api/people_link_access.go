package api

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// tellNemuraAccess tells a linked Nemura what each sign-in identity of a user may now enter in Fyniq: whether they hold a role in the finance workspace and whether a passkey is enrolled (one sign-in across
// the apps, design D7). It is queued in the unit of work of the change that moved either (a role given or ended, a member removed or joined, a passkey enrolled or revoked, the link approved), through the
// link's one event scope, by issuer and subject and never an email or a role name. Nothing is queued with no active link that has keys. A door and a hint, never a grant: Nemura's own sign-in and roles
// decide what a person may do there.
// CrossOnlyMappedMembers is the one switch of what crosses to Nemura (owner's question of 21 Nov 2026, built on the safer answer, owner to confirm): true, only a member mapped to a person of the link
// (mapPersonToUser) has their identities' access told to Nemura, so no issuer or subject of someone Nemura does not know leaves Fyniq; false, every member's does, which is what a Nemura approver with no
// mapping needs to be reminded of a missing passkey and to see the switch. Flipping it is this constant and nothing else.
const CrossOnlyMappedMembers = true

func (a *API) tellNemuraAccess(ctx context.Context, u storage.UnitOfWork, user uuid.UUID) error {
	l, err := u.PeopleLinks().Live(ctx)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && l.State != storage.LinkActive) {
		return nil
	}
	if err != nil {
		return err
	}
	if CrossOnlyMappedMembers {
		if _, err := u.PeopleLinks().PersonOfUser(ctx, user); errors.Is(err, ports.ErrNotFound) {
			return nil // nobody mapped them to a person Nemura knows: their identity stays here
		} else if err != nil {
			return err
		}
	}
	sessions, ok := a.deps.Installation.(storage.Sessions)
	if !ok {
		return nil
	}
	ids, err := sessions.Identities(ctx, user)
	if err != nil {
		return err
	}
	members, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	finance := false
	if i := slices.IndexFunc(members, func(m storage.Member) bool { return m.UserID == user }); i >= 0 {
		roles := make([]authz.Role, len(members[i].Roles))
		for j, r := range members[i].Roles {
			roles[j] = authz.Role(r.Role)
		}
		finance = financeWorkspace(roles)
	}
	keys, err := u.Passkeys().List(ctx, user, false)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := linksend.Queue(ctx, u, linksend.TypeIdentityAccess, "identity", user.String(), 0,
			linksend.IdentityAccess{Issuer: id.Issuer, Subject: id.Subject, Finance: finance, Passkey: len(keys) > 0}); err != nil {
			return err
		}
	}
	return nil
}

// tellNemuraEveryone tells Nemura what every member's identities may enter, when the link is approved, so its switch and its passkey warning are right from the first day.
func (a *API) tellNemuraEveryone(ctx context.Context, u storage.UnitOfWork) error {
	members, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	for _, m := range members {
		if err := a.tellNemuraAccess(ctx, u, m.UserID); err != nil {
			return err
		}
	}
	return nil
}
