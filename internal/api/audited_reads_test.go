package api

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/domain/audit"
)

// Every operation that carries a file or reveals a full identifier is audited (AUD-05, p1-audit-explorer design D3):
// it is in auditedReads, with a security event's action (AUD-06), or in unauditedFiles with why. An operation added that
// returns a file (content_base64), or is named as an export or a reveal, fails here until it says which.
// req: AUD-05
// req: AUD-06
func TestEveryFileAndRevealIsAudited(t *testing.T) {
	a := testAPI(t)
	schemas := a.OpenAPI().Components.Schemas.Map()
	var carries func(s *huma.Schema, seen map[string]bool) bool
	carries = func(s *huma.Schema, seen map[string]bool) bool {
		if s == nil {
			return false
		}
		if s.Ref != "" {
			name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
			if seen[name] {
				return false
			}
			seen[name] = true
			return carries(schemas[name], seen)
		}
		if _, ok := s.Properties["content_base64"]; ok {
			return true
		}
		for _, x := range append(append(append([]*huma.Schema{s.Items}, s.AllOf...), s.OneOf...), s.AnyOf...) {
			if carries(x, seen) {
				return true
			}
		}
		for _, p := range s.Properties {
			if carries(p, seen) {
				return true
			}
		}
		return false
	}
	ops := map[string]bool{}
	found := 0
	for _, op := range operations(a) {
		ops[op.OperationID] = true
		file := false
		for code, r := range op.Responses {
			if strings.HasPrefix(code, "2") && r.Content["application/json"] != nil && carries(r.Content["application/json"].Schema, map[string]bool{}) {
				file = true
			}
		}
		if !file && !strings.HasPrefix(op.OperationID, "reveal") && !strings.HasPrefix(op.OperationID, "export") {
			continue
		}
		found++
		action, audited := auditedReads[op.OperationID]
		why, exempt := unauditedFiles[op.OperationID]
		switch {
		case audited && exempt:
			t.Errorf("%s is both audited and exempt", op.OperationID)
		case audited && !audit.IsSecurity(action):
			t.Errorf("%s writes %s, which is not a security event, so its IP and device would stay in the trail for life (AUD-06)", op.OperationID, action)
		case exempt && len(why) < 40:
			t.Errorf("%s is exempt without saying why", op.OperationID)
		case !audited && !exempt:
			t.Errorf("%s carries a file or a full identifier: add the audit event it writes to auditedReads (AUD-05)", op.OperationID)
		}
	}
	if found < 12 {
		t.Fatalf("only %d operations carry files or identifiers: the walk no longer finds them", found)
	}
	for op := range auditedReads {
		if !ops[op] {
			t.Errorf("auditedReads names %s, which is no operation", op)
		}
	}
	for op := range unauditedFiles {
		if !ops[op] {
			t.Errorf("unauditedFiles names %s, which is no operation", op)
		}
	}
}
