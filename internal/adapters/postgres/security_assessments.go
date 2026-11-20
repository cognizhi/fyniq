package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The register of security assessments (SEC-25; 001301): assessments, findings and their steps are only ever added, each audited.

func (c compliance) Assessments(ctx context.Context) ([]storage.Assessment, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListSecurityAssessments(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	fs, err := c.u.q.ListSecurityFindings(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	ss, err := c.u.q.ListSecurityFindingSteps(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	steps := map[uuid.UUID][]storage.FindingStep{}
	for _, s := range ss {
		steps[s.FindingID] = append(steps[s.FindingID], storage.FindingStep{ID: s.ID, FindingID: s.FindingID, Step: s.Step, OnDate: dateOf(s.OnDate),
			Reason: str(s.Reason), RecordedBy: s.RecordedBy, RecordedAt: s.RecordedAt})
	}
	found := map[uuid.UUID][]storage.Finding{}
	for _, f := range fs {
		x := storage.Finding{ID: f.ID, AssessmentID: f.AssessmentID, Ref: f.Ref, Title: f.Title, Severity: f.Severity, FoundOn: dateOf(f.FoundOn),
			RecordedBy: f.RecordedBy, RecordedAt: f.RecordedAt, Steps: steps[f.ID]}
		if f.DueOn.Valid {
			d := dateOf(f.DueOn.Time)
			x.DueOn = &d
		}
		found[f.AssessmentID] = append(found[f.AssessmentID], x)
	}
	out := make([]storage.Assessment, len(rows))
	for i, r := range rows {
		out[i] = storage.Assessment{ID: r.ID, Kind: r.Kind, Tester: r.Tester, Independent: r.Independent, Scope: r.Scope, StartedOn: dateOf(r.StartedOn),
			EndedOn: dateOf(r.EndedOn), Summary: r.Summary, ReportSHA256: str(r.ReportSha256), ReportLocation: str(r.ReportLocation), RecordedBy: r.RecordedBy,
			RecordedAt: r.RecordedAt, Findings: found[r.ID]}
	}
	return out, nil
}

func (c compliance) RecordAssessment(ctx context.Context, a storage.Assessment) (storage.Assessment, error) {
	if err := c.u.check(); err != nil {
		return storage.Assessment{}, err
	}
	id, err := c.u.q.InsertSecurityAssessment(ctx, sqlcdb.InsertSecurityAssessmentParams{Kind: a.Kind, Tester: a.Tester, Independent: a.Independent, Scope: a.Scope,
		StartedOn: a.StartedOn, EndedOn: a.EndedOn, Summary: a.Summary, ReportSha256: strPtr(a.ReportSHA256), ReportLocation: strPtr(a.ReportLocation),
		RecordedBy: a.RecordedBy, RecordedAt: a.RecordedAt})
	if err != nil {
		return storage.Assessment{}, mapError(err)
	}
	a.ID = id
	if err := c.u.event(ctx, "security_assessment.recorded", "security_assessment", id, nil, map[string][2]any{"kind": {nil, a.Kind}, "tester": {nil, a.Tester},
		"independent": {nil, a.Independent}, "started_on": {nil, day(a.StartedOn)}, "ended_on": {nil, day(a.EndedOn)}, "report_sha256": {nil, a.ReportSHA256}}); err != nil {
		return storage.Assessment{}, err
	}
	findings := a.Findings
	a.Findings = nil
	for _, f := range findings {
		saved, err := c.AddFinding(ctx, id, f)
		if err != nil {
			return storage.Assessment{}, err
		}
		a.Findings = append(a.Findings, saved)
	}
	return a, nil
}

func (c compliance) AddFinding(ctx context.Context, assessment uuid.UUID, f storage.Finding) (storage.Finding, error) {
	if err := c.u.check(); err != nil {
		return storage.Finding{}, err
	}
	if ok, err := c.u.q.SecurityAssessmentExists(ctx, assessment); err != nil || !ok {
		if err != nil {
			return storage.Finding{}, mapError(err)
		}
		return storage.Finding{}, ports.ErrNotFound
	}
	p := sqlcdb.InsertSecurityFindingParams{AssessmentID: assessment, Ref: f.Ref, Title: f.Title, Severity: f.Severity, FoundOn: f.FoundOn,
		RecordedBy: f.RecordedBy, RecordedAt: f.RecordedAt, DueOn: pgDate(f.DueOn)}
	id, err := c.u.q.InsertSecurityFinding(ctx, p)
	if err != nil {
		return storage.Finding{}, mapError(err)
	}
	f.ID, f.AssessmentID = id, assessment
	return f, c.u.event(ctx, "security_finding.recorded", "security_finding", id, nil, map[string][2]any{"assessment_id": {nil, assessment.String()}, "ref": {nil, f.Ref},
		"title": {nil, f.Title}, "severity": {nil, f.Severity}, "found_on": {nil, day(f.FoundOn)}, "due_on": {nil, dayOf(f.DueOn)}})
}

func (c compliance) StepFinding(ctx context.Context, finding uuid.UUID, s storage.FindingStep) (storage.FindingStep, error) {
	if err := c.u.check(); err != nil {
		return storage.FindingStep{}, err
	}
	if ok, err := c.u.q.SecurityFindingExists(ctx, finding); err != nil || !ok {
		if err != nil {
			return storage.FindingStep{}, mapError(err)
		}
		return storage.FindingStep{}, ports.ErrNotFound
	}
	id, err := c.u.q.InsertSecurityFindingStep(ctx, sqlcdb.InsertSecurityFindingStepParams{FindingID: finding, Step: s.Step, OnDate: s.OnDate,
		Reason: strPtr(s.Reason), RecordedBy: s.RecordedBy, RecordedAt: s.RecordedAt})
	if err != nil {
		return storage.FindingStep{}, mapError(err)
	}
	s.ID, s.FindingID = id, finding
	changes := map[string][2]any{"step": {nil, s.Step}, "on": {nil, day(s.OnDate)}}
	if s.Reason != "" {
		changes["reason"] = [2]any{nil, s.Reason}
	}
	return s, c.u.event(ctx, "security_finding.stepped", "security_finding", finding, nil, changes)
}
