//go:build local

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The organisation's dimensions and their values through the unit of work (FR-DIM-01): added, listed with Department
// and Project before the custom ones by place, and each value by its dimension then code; renamed and put out of use
// one version at a time, a stale version refused; every change audited on the dimension, a value's naming the value;
// another organisation sees none of it.
// req: FR-DIM-01
// req: AUD-01
// req: INV-06
func TestDimensionsAreKeptWithTheirAuditTrail(t *testing.T) {
	ctx := context.Background()
	a := books(t, "Dimensions A Pte Ltd")
	b := books(t, "Dimensions B Pte Ltd")
	st := store(t, "fyniq_api")
	var region, dept, proj storage.Dimension
	var eng, ops, atlas storage.DimensionValue
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		d := u.Dimensions()
		if region, err = d.Create(ctx, storage.NewDimension{Kind: storage.DimensionCustom, Place: 2, Name: "Region"}); err != nil {
			return err
		}
		if proj, err = d.Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"}); err != nil {
			return err
		}
		if dept, err = d.Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"}); err != nil {
			return err
		}
		if ops, err = d.CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "OPS", Name: "Operations"}); err != nil {
			return err
		}
		if atlas, err = d.CreateValue(ctx, storage.NewDimensionValue{DimensionID: proj.ID, Code: "ATLAS", Name: "Atlas"}); err != nil {
			return err
		}
		eng, err = d.CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !region.Active || region.Version != 1 || region.Place != 2 || dept.Place != 0 || eng.DimensionID != dept.ID || !eng.Active {
		t.Errorf("as added: %+v %+v %+v", region, dept, eng)
	}

	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ds, err := u.Dimensions().List(ctx)
		if err != nil {
			return err
		}
		var names []string
		for _, d := range ds {
			names = append(names, d.Name)
		}
		if !slices.Equal(names, []string{"Department", "Project", "Region"}) {
			t.Errorf("the dimensions in order: %v", names)
		}
		vs, err := u.Dimensions().Values(ctx, nil)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, v := range vs {
			ids = append(ids, v.ID)
		}
		if !slices.Equal(ids, []uuid.UUID{eng.ID, ops.ID, atlas.ID}) {
			t.Errorf("every value, by dimension then code: %v", vs)
		}
		if vs, err = u.Dimensions().Values(ctx, &proj.ID); err != nil || len(vs) != 1 || vs[0].ID != atlas.ID {
			t.Errorf("the project's values: %v %v", vs, err)
		}
		if _, err := u.Dimensions().Get(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("no such dimension: %v", err)
		}
		if _, err := u.Dimensions().GetValue(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("no such value: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if region, err = u.Dimensions().Change(ctx, region.ID, 1, storage.DimensionChange{Name: "Market", Active: false}); err != nil {
			return err
		}
		ops, err = u.Dimensions().ChangeValue(ctx, ops.ID, 1, storage.DimensionChange{Name: "Operations & finance", Active: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if region.Name != "Market" || region.Active || region.Version != 2 || region.Kind != storage.DimensionCustom || region.Place != 2 || ops.Version != 2 || ops.Code != "OPS" {
		t.Errorf("after the changes: %+v %+v", region, ops)
	}
	var se *storage.Error
	err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Dimensions().Change(ctx, region.ID, 1, storage.DimensionChange{Name: "Region", Active: true})
		return err
	})
	if !errors.As(err, &se) || se.Code != storage.CodeVersion {
		t.Errorf("a stale version of a dimension: %v", err)
	}
	err = st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Dimensions().ChangeValue(ctx, ops.ID, 1, storage.DimensionChange{Name: "Ops", Active: true})
		return err
	})
	if !errors.As(err, &se) || se.Code != storage.CodeVersion {
		t.Errorf("a stale version of a value: %v", err)
	}
	err = st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Teams"})
		return err
	})
	if !errors.As(err, &se) || se.Code != storage.CodeDuplicate {
		t.Errorf("a second Department at the database: %v", err)
	}

	if got := actions(t, a.Org, region.ID); !slices.Equal(got, []string{"dimension.created", "dimension.changed"}) {
		t.Errorf("the region's audit trail: %v", got)
	}
	if got := actions(t, a.Org, dept.ID); !slices.Equal(got, []string{"dimension.created", "dimension.value_added", "dimension.value_added", "dimension.value_changed"}) {
		t.Errorf("the department's audit trail, its values' included: %v", got)
	}

	if err := st.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ds, err := u.Dimensions().List(ctx)
		if err == nil && len(ds) > 0 {
			t.Errorf("another organisation's dimensions: %v", ds)
		}
		if _, err := u.Dimensions().GetValue(ctx, eng.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation's value: %v", err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
