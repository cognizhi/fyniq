// Package superadmin checks the installation's Super admins (PRD-001 FR-ADM-07, CTL-IAM-07): the
// nightly count of Super admins who can sign in, which must be at least one.
package superadmin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Control is the control the check evidences.
const Control = "CTL-IAM-07"

// Verification is one night's count.
type Verification struct {
	SetUp  bool // false before first-run setup: there is nothing to count yet
	Active int  // active Super admins who can sign in
}

// Verify counts the active Super admins. None is a failure: it is recorded in the installation's trail
// as a failed control check and returned as an error, so the job's alert fires. Open question 36 wants
// two, one per founder; the setup wizard asks for the second, and one is not a failure.
func Verify(ctx context.Context, store storage.Store, inst storage.Installation) (Verification, error) {
	org, err := inst.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return Verification{}, nil
	}
	if err != nil {
		return Verification{}, err
	}
	n, err := inst.CountSuperAdmins(ctx)
	if err != nil {
		return Verification{}, err
	}
	v := Verification{SetUp: true, Active: n}
	if n >= 1 {
		return v, nil
	}
	if err := record(ctx, store, org, n); err != nil {
		return v, err
	}
	return v, fmt.Errorf("the installation has no active Super admin (%s): follow docs/runbooks/super-admin-recovery.md", Control)
}

func record(ctx context.Context, store storage.Store, org uuid.UUID, n int) error {
	return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Audit().Record(ctx, storage.AuditRecord{Action: balance.ActionCheckFailed, ObjectType: "organisation", ObjectID: org,
			Changes: map[string][2]any{"control": {nil, Control}, "check": {nil, "superadmins.verify"}, "active_super_admins": {nil, n}}})
	})
}
