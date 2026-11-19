// Package role names the services the one nemura image runs as (PRD-002 ADR-N1, NM-ARC-02, NM-ARC-06): the same
// static binary starts as the API, the worker, the proposal executor, the people assistant or the migration job,
// chosen by NEMURA_ROLE, so each service has its own identity, database role and scaling from one image and one
// digest. A role that is unset, unknown or not built yet refuses to start with a stable code and a plain fix.
package role

import (
	"fmt"
	"strings"

	"github.com/cognizhi/fyniq/internal/runtime"
)

// Env is the environment variable that names the role.
const Env = "NEMURA_ROLE"

// Role is one way the nemura image starts.
type Role string

// The roles of NM-ARC-06 and the migration job.
const (
	API       Role = "api"       // the REST API under /v1 and, later, the embedded web app
	Migrate   Role = "migrate"   // the run-to-completion job that applies Nemura's migrations
	Worker    Role = "worker"    // background work from the outbox (ADR-T7)
	Executor  Role = "executor"  // applies approved proposals through domain commands
	Assistant Role = "assistant" // the people assistant's runtime: reads through tools, only inserts proposals
)

// All lists every role, in the order the plan builds them.
var All = []Role{API, Migrate, Worker, Executor, Assistant}

// Built are the roles this build can run. A role joins it with the change that builds it.
var Built = []Role{API, Migrate}

// Code is the stable code of every refusal.
const Code = "NEMURA-ROLE-01"

func names(roles []Role) string {
	s := make([]string, len(roles))
	for i, r := range roles {
		s[i] = string(r)
	}
	return strings.Join(s, ", ")
}

func built(r Role) bool {
	for _, b := range Built {
		if b == r {
			return true
		}
	}
	return false
}

// Parse reads the value of NEMURA_ROLE.
func Parse(value string) (Role, error) {
	r := Role(strings.TrimSpace(value))
	switch {
	case r == "":
		return "", &runtime.Error{Code: Code, Message: fmt.Sprintf("%s is not set: set it to the service this container runs as (%s)", Env, names(Built))}
	case !containsRole(All, r):
		return "", &runtime.Error{Code: Code, Message: fmt.Sprintf("%s=%q is not a role: use one of %s", Env, value, names(All))}
	case !built(r):
		return "", &runtime.Error{Code: Code, Message: fmt.Sprintf("the %s role is not built yet in this release: use one of %s", r, names(Built))}
	}
	return r, nil
}

func containsRole(roles []Role, r Role) bool {
	for _, x := range roles {
		if x == r {
			return true
		}
	}
	return false
}

// Service is the name /version reports for a role: nemura-api, nemura-worker, ….
func (r Role) Service() string { return "nemura-" + string(r) }

// DatabaseRole is the PostgreSQL role the service connects as (PRD-001 SEC-03; db/nemura/migrations): one per service, so
// no service holds another's grants. The migrate job connects as the one role that owns the schema.
func (r Role) DatabaseRole() string {
	if r == Migrate {
		return "nemura_migrator"
	}
	return "nemura_" + string(r)
}
