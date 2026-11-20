package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/role"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ownerRecovery is the deployment's two-person recovery of an Owner (the migrate role; `make people-owner-recovery`): both operators are present
// and named, the one-time code is printed once and never logged, and the organisation's audit trail records both names (FR-ADM-09, SEC-35 as
// Fyniq's `make super-admin-recovery`).
func ownerRecovery(ctx context.Context, getenv func(string) string, args []string, clock ports.Clock) error {
	if len(args) != 3 {
		return errors.New(`usage: NEMURA_ROLE=migrate nemura owner-recovery <organisation id> <member id> "<name>, <name>"`)
	}
	org, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("the organisation ID %q is not an ID", args[0])
	}
	member, err := uuid.Parse(args[1])
	if err != nil {
		return fmt.Errorf("the member ID %q is not an ID", args[1])
	}
	operators, err := identity.Operators(args[2])
	if err != nil {
		return err
	}
	url, err := databaseURL(role.Migrate, getenv)
	if err != nil {
		return err
	}
	code, expires, err := nemura.IssueOwnerRecovery(ctx, url, org, member, operators, clock.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "The one-time recovery code for the Owner (shown once; valid until %s; the Owner signs in with the account they will keep, chooses \"I have a recovery code\" and types it):\n", expires.Format("2006-01-02 15:04 MST"))
	fmt.Println(code)
	return nil
}

// linkRestore is the deployment's step after a restore of Nemura's database and before it opens (NM-INT-13; docs/runbooks): two named operators record the restore, which suspends the link and lists the
// runs whose answer from Fyniq the restored database does not hold, to be decided against Fyniq's own record by an Owner or an Administrator before anything is sent or paid.
func linkRestore(ctx context.Context, getenv func(string) string, args []string, clock ports.Clock) error {
	if len(args) != 2 {
		return errors.New(`usage: NEMURA_ROLE=migrate nemura link-restore <organisation id> "<name>, <name>"`)
	}
	org, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("the organisation ID %q is not an ID", args[0])
	}
	operators, err := identity.Operators(args[1])
	if err != nil {
		return err
	}
	url, err := databaseURL(role.Migrate, getenv)
	if err != nil {
		return err
	}
	held, linked, err := nemura.RecordLinkRestore(ctx, url, org, operators, clock.Now())
	if err != nil {
		return err
	}
	if !linked {
		fmt.Fprintln(os.Stderr, "That organisation has no link to Fyniq past its proposal: nothing to reconcile.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "The link is suspended. %d run(s) approved with proof have no answer from Fyniq in the restored database: an Owner or an Administrator decides each on the link's page against Fyniq's own record, and the link resumes.\n", held)
	return nil
}

// erasureReapply is the deployment's step after a restore of Nemura's database and before it opens (NM-SEC-08, NM-INT-13): every signed entry of the register of erasures is applied again, so a personal field erased
// since the backup does not come back. An entry that does not verify is reported and not applied; running it again is safe.
func erasureReapply(ctx context.Context, getenv func(string) string, args []string, clock ports.Clock) error {
	if len(args) != 1 {
		return errors.New("usage: NEMURA_ROLE=migrate nemura erasure-reapply <organisation id>")
	}
	org, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("the organisation ID %q is not an ID", args[0])
	}
	register, signer := erasureWiring(getenv)
	if register == nil || signer == nil {
		return errors.New("this deployment has no register of erasures: nothing can be applied again")
	}
	url, err := databaseURL(role.Migrate, getenv)
	if err != nil {
		return err
	}
	store, err := nemura.Open(ctx, url)
	if err != nil {
		return err
	}
	defer store.Close()
	n, err := (&nemura.ErasureRunner{Store: store, Blobs: register, Signer: signer, Clock: clock}).Reapply(ctx, org)
	fmt.Fprintf(os.Stderr, "%d person(s) erased again from the register.\n", n)
	return err
}
