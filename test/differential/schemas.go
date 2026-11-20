// Package differential embeds the shared scenario and result schemas, so the Go side of the
// differential tests validates against the same files as the oracle (task 2.1).
package differential

import _ "embed"

// ScenarioSchema is scenario.schema.json (fyniq-scenario@1).
//
//go:embed scenario.schema.json
var ScenarioSchema []byte

// ResultSchema is result.schema.json (fyniq-scenario-result@1).
//
//go:embed result.schema.json
var ResultSchema []byte

// ForecastSchema is forecast.schema.json (fyniq-forecast@1).
//
//go:embed forecast.schema.json
var ForecastSchema []byte

// FeasibilitySchema is feasibility.schema.json (fyniq-feasibility@1).
//
//go:embed feasibility.schema.json
var FeasibilitySchema []byte

// PurgeSchema is purge.schema.json (fyniq-purge@1).
//
//go:embed purge.schema.json
var PurgeSchema []byte

// PayrollMonthSchema is payroll.schema.json (fyniq-payroll-month@1): an employee-month the Go engine of Nemura's payroll and the oracle both work out (p1-nemura-payroll).
//
//go:embed payroll.schema.json
var PayrollMonthSchema []byte

// PayrollRunSchema is payroll-run.schema.json (fyniq-payroll-run@1): a pay run of one wage month, each person's employee-month, the run's totals and its entry (p1-nemura-payroll).
//
//go:embed payroll-run.schema.json
var PayrollRunSchema []byte

// PartMonthSchema is part-month.schema.json: one hand-worked case of a part month's pay in working days (NM-PAY-12).
//
//go:embed part-month.schema.json
var PartMonthSchema []byte

// SubscriptionSchema is subscription.schema.json: one hand-worked set of subscription contracts and the contracted MRR on given days (FR-AR-15, FR-KPI-02).
//
//go:embed subscription.schema.json
var SubscriptionSchema []byte

// ProjectRevenueSchema is project-revenue.schema.json: one hand-worked case of a fixed-fee project's revenue over time by cost-to-cost progress (FR-PRJ-11, RND-RECOGNISE).
//
//go:embed project-revenue.schema.json
var ProjectRevenueSchema []byte

// AllocationSchema is allocation.schema.json: one case of a shared cost split over the targets of an allocation rule, with what the key cannot place left unallocated (FR-DIM-03, ALLOC-PRORATA).
//
//go:embed allocation.schema.json
var AllocationSchema []byte

// ShareServiceSchema is share-service.schema.json: one case of the expense of shares issued for services, a grant's tranches spread over their service months (FR-EQ-13, ALLOC-EVEN, RND-LINE).
//
//go:embed share-service.schema.json
var ShareServiceSchema []byte
