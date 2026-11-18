package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAIQualification is the database's refusal of a qualification's change (FY339): one changed or removed, or put
// out of date twice (PRD-001 AI-78; p1-ai-providers task 5.4).
const CodeAIQualification = "STORAGE-AI-QUALIFICATION-01"

// AIQualification is a run of an agent's eval suite against a connection's model (AI-78).
type AIQualification struct {
	ID           uuid.UUID
	Agent        string
	ConnectionID uuid.UUID
	Model        string
	Version      string // the version the provider reported while it ran; "" when none
	Suite        string
	SuiteVersion int
	Threshold    string // the share of cases that must pass, as decimal text
	Cases        int
	Passed       int
	Result       string // pass or fail
	Run          uuid.UUID
	RanBy        string
	RanAt        time.Time
	OutOfDateAt  *time.Time
	OutOfDateWhy string
}

// AIQualifications keeps the qualifications.
type AIQualifications interface {
	// Latest are a connection's latest qualification of each model for each agent.
	Latest(ctx context.Context, connection uuid.UUID) ([]AIQualification, error)
	// Record keeps a run: audited as ai_qualification.recorded.
	Record(ctx context.Context, q AIQualification) (AIQualification, error)
	// OutOfDate marks a qualification out of date, once, with why (AI-87): audited as ai_qualification.out_of_date.
	OutOfDate(ctx context.Context, id uuid.UUID, why string, at time.Time) error
	// Due are the passing qualifications, in every connection, that ran before at or are out of date: what the weekly
	// re-qualification runs again.
	Due(ctx context.Context, before time.Time) ([]AIQualification, error)
}
