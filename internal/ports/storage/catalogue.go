package storage

import "context"

// Guards are what the database itself enforces, each as a line of the schema tests' snapshots: every role's privileges
// (grants.golden), the triggers and row-level security policies, and which tables force row-level security
// (schema.golden). The nightly schema.verify compares the database's with the release's (CTL-FIN-02, CTL-AUD-01,
// CTL-AI-01; p1-compliance-center design D4).
type Guards struct {
	Grants      []string
	Triggers    []string
	Policies    []string
	RowSecurity []string
}

// Catalogue reads the database's own catalogue. Only a database store has one.
type Catalogue interface {
	Guards(ctx context.Context) (Guards, error)
}
