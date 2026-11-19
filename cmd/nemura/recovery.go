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
