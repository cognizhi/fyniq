package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The statuses and billing methods of a project (FR-PRJ-01, FR-PRJ-10).
const (
	ProjectPlanned = "planned"
	ProjectActive  = "active"
	ProjectOnHold  = "on_hold"
	ProjectClosed  = "closed"

	BillingTimeAndMaterials = "time_and_materials"
	BillingFixedFee         = "fixed_fee"
	BillingRetainer         = "retainer"
	BillingInternal         = "internal"
)

// Project is a value of the Project dimension with its attributes (FR-PRJ-01); its ID is the value's ID, so every line
// already tagged names it. Configured is false for a value of the dimension that has no attribute row yet: its other
// fields are then empty and Version is 0.
type Project struct {
	ID            uuid.UUID
	DimensionID   uuid.UUID
	Code, Name    string
	Active        bool // its value is in use
	ValueVersion  int
	Configured    bool
	CustomerID    *uuid.UUID
	Currency      string
	MinorUnits    int
	Status        string
	BillingMethod string
	StartsOn      *time.Time
	EndsOn        *time.Time
	BudgetMinutes *int
	BudgetAmount  *Decimal // in the project's currency
	Version       int      // the attribute row's version, for If-Match; 0 when not configured
	CreatedAt     time.Time
	ChangedAt     time.Time
}

// NewProject writes the attribute row of a value of the Project dimension.
type NewProject struct {
	ID            uuid.UUID
	CustomerID    *uuid.UUID
	Currency      string
	Status        string
	BillingMethod string
	StartsOn      *time.Time
	EndsOn        *time.Time
	BudgetMinutes *int
	BudgetAmount  *Decimal
}

// ProjectChange is what changes of a project's attributes; its value, its currency and its creation never do.
type ProjectChange struct {
	CustomerID    *uuid.UUID
	Status        string
	BillingMethod string
	StartsOn      *time.Time
	EndsOn        *time.Time
	BudgetMinutes *int
	BudgetAmount  *Decimal
}

// ProjectMember is a person on a project: a member logs time on it, a manager also approves it (FR-PRJ-07).
type ProjectMember struct {
	ProjectID uuid.UUID
	UserID    uuid.UUID
	Manager   bool
	Active    bool
}

// ProjectPerson names a person of a project as it is set: the set replaces the project's members.
type ProjectPerson struct {
	UserID  uuid.UUID
	Manager bool
}

// ProjectTask is what time on a project is logged against.
type ProjectTask struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Billable  bool
	Active    bool
	Version   int
}

// Projects are the organisation's projects, their members and their tasks (FR-PRJ-01). Each write records its audit
// event under the project in the unit of work (AUD-01); nothing is deleted.
type Projects interface {
	// List is every value of the Project dimension, configured or not, by code. Get is one, ports.ErrNotFound for none.
	List(ctx context.Context) ([]Project, error)
	Get(ctx context.Context, id uuid.UUID) (Project, error)
	// Create writes the attribute row of an existing value of the Project dimension.
	Create(ctx context.Context, n NewProject) (Project, error)
	// Change changes a project's attributes at its version: CodeVersion when it changed since.
	Change(ctx context.Context, id uuid.UUID, version int, c ProjectChange) (Project, error)
	// Members are a project's members, those put out of use included, managers first then by person.
	Members(ctx context.Context, id uuid.UUID) ([]ProjectMember, error)
	// SetMembers replaces the project's people: a person not named is put out of use, one named again is back in use.
	SetMembers(ctx context.Context, id uuid.UUID, people []ProjectPerson) error
	// OfUser are the projects a person is an active member or manager of.
	OfUser(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error)
	// Tasks are a project's tasks, those out of use included, by name. GetTask is one, ports.ErrNotFound for none.
	Tasks(ctx context.Context, id uuid.UUID) ([]ProjectTask, error)
	GetTask(ctx context.Context, id uuid.UUID) (ProjectTask, error)
	CreateTask(ctx context.Context, project uuid.UUID, name string, billable bool) (ProjectTask, error)
	// ChangeTask changes a task at its version: CodeVersion when it changed since.
	ChangeTask(ctx context.Context, id uuid.UUID, version int, name string, billable, active bool) (ProjectTask, error)
}

// CodeProject: a project is a value of the Project dimension for a customer, keeps its value, currency and creation,
// changes one version at a time and is never deleted; a member is an active member of the organisation and neither a
// member nor a task is deleted; a closed project takes no new task (FR-PRJ-01; FY600, FY601).
const CodeProject = "STORAGE-PRJ-01"
