package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
)

// An operation opts out of a convention of §8.18.1 only with x-fyniq-exempt, and only these do: the
// commands whose response carries a one-time secret, and (rev 0.35) an invitation to an organisation and its
// redemption, take no Idempotency-Key (FR-INT-08, rev 0.27) and no preview (FR-INT-14, rev 0.28). A new exemption fails this test until it is added here, in a
// reviewed change.
// req: FR-INT-08
// req: FR-INT-14
func TestConventionExemptions(t *testing.T) {
	var want []string
	for _, op := range []string{"acceptMemberInvitation", "acceptSuperAdminInvitation", "completeSetup", "confirmSuperAdminRecovery",
		"createMemberInvitation", "createSuperAdminInvitation", "recoverSuperAdmin", "replaceSuperAdminRecoveryCodes"} {
		want = append(want, op+" Idempotency-Key", op+" Prefer")
	}
	for _, op := range []string{"challengeApproval", "registerPasskey", "startPasskeyRegistration", "startStepUp", "stepUp"} { // a challenge makes them one-time
		want = append(want, op+" Idempotency-Key", op+" Prefer")
	}
	want = append(want, "revokePasskey Prefer", "signOutDevice Prefer", "signOutOtherDevices Idempotency-Key", "signOutOtherDevices Prefer")
	// A preview saves nothing, so a repeat is harmless and return=preview would answer the same (rev 0.97).
	want = append(want, "previewDocumentTemplate Idempotency-Key", "previewDocumentTemplate Prefer")
	// A back-test saves nothing either (FR-BNK-05).
	want = append(want, "backTestBankRule Idempotency-Key", "backTestBankRule Prefer")
	// Reconcile now posts each line in its own transaction; a repeat finds them no longer waiting (FR-BNK-14, rev 0.143).
	want = append(want, "autoReconcileBankLines Idempotency-Key")
	// A reveal changes no record: a repeat shows the number again and records another look (FR-CT-03, rev 0.153).
	want = append(want, "revealPayeeBankAccount Idempotency-Key", "revealPayeeBankAccount Prefer", "revealPayeeBankAccount If-Match")
	slices.Sort(want)
	var got []string
	for _, op := range operations(testAPI(t)) {
		ex, _ := op.Extensions["x-fyniq-exempt"].(map[string]string)
		for convention, reason := range ex {
			got = append(got, op.OperationID+" "+convention)
			if len(reason) < 40 {
				t.Errorf("%s is exempt from %s without a reason", op.OperationID, convention)
			}
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("exemptions:\n got %v\nwant %v", got, want)
	}
}

// A deprecated operation announces its sunset on every response, with at least six months' notice,
// and its contract says so (FR-INT-06).
// req: FR-INT-06
func TestDeprecatedOperationsAnnounceTheirSunset(t *testing.T) {
	mux := http.NewServeMux()
	a := New(mux, Deps{Auth: authn.NotYet{}, Version: "0.0.0"})
	note := "https://docs.fyniq.example/migrations/old-report"
	huma.Register(a.API, deprecate(huma.Operation{
		OperationID: "getOldReport", Method: http.MethodGet, Path: "/v1/old-report", Summary: "An old report",
	}, "2027-01-31", "2027-07-31", note), func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/old-report", nil))
	h := rec.Header()
	// 31 Jan 2027 00:00 UTC is 1801353600 seconds after the epoch.
	if h.Get("Deprecation") != "@1801353600" || h.Get("Sunset") != "Sat, 31 Jul 2027 00:00:00 GMT" ||
		h.Get("Link") != "<"+note+`>; rel="deprecation"; type="text/html"` {
		t.Errorf("a deprecated operation's response (%d) carries %v", rec.Code, h)
	}
	other := httptest.NewRecorder()
	mux.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/v1/organisation", nil))
	if other.Header().Get("Deprecation") != "" || other.Header().Get("Sunset") != "" {
		t.Errorf("an operation that is not deprecated carries %v", other.Header())
	}

	op := a.OpenAPI().Paths["/v1/old-report"].Get
	if !op.Deprecated || op.Extensions["x-sunset"] != "2027-07-31" || op.Extensions["x-fyniq-deprecated-on"] != "2027-01-31" {
		t.Errorf("the contract does not mark the deprecation: %v %v", op.Deprecated, op.Extensions)
	}
	for status, r := range op.Responses {
		for _, name := range []string{"Deprecation", "Sunset", "Link"} {
			if r.Headers[name] == nil {
				t.Errorf("the %s response does not document %s", status, name)
			}
		}
	}

	// Less than six months' notice, an unparsable date, or a note that is not an https page is refused.
	for _, c := range []struct{ on, sunset, note string }{
		{"2027-01-31", "2027-07-30", note},
		{"2027-08-31", "2028-02-29", note}, // six months after 31 Aug is 2 Mar, as time.AddDate counts
		{"31 Jan 2027", "2027-07-31", note},
		{"2027-01-31", "2027-07-31", "http://docs.fyniq.example/migrations"},
	} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), "getOldReport") {
					t.Errorf("deprecate(%q, %q, %q) was accepted: %v", c.on, c.sunset, c.note, r)
				}
			}()
			deprecate(huma.Operation{OperationID: "getOldReport"}, c.on, c.sunset, c.note)
		}()
	}
}

// Lists held in memory page like the database's: in their own order, with a cursor naming the last
// item (FR-INT-10).
// req: FR-INT-10
func TestInMemoryListsPage(t *testing.T) {
	ids := []string{"google", "zoho-us", "zoho-eu", "local"}
	key := func(s string) string { return s }
	first, next, err := pageOf(ids, key, PageQuery{PageSize: 3})
	if err != nil || !slices.Equal(first, ids[:3]) || next == "" {
		t.Fatalf("first page: %v %q %v", first, next, err)
	}
	rest, last, err := pageOf(ids, key, PageQuery{PageSize: 3, Cursor: next})
	if err != nil || !slices.Equal(rest, ids[3:]) || last != "" {
		t.Fatalf("second page: %v %q %v", rest, last, err)
	}
	all, none, err := pageOf(ids, key, PageQuery{})
	if err != nil || !slices.Equal(all, ids) || none != "" {
		t.Fatalf("the default page: %v %q %v", all, none, err)
	}
	for _, bad := range []string{"bm90LWEtY3Vyc29y", encodeCursor("gone")} {
		if _, _, err := pageOf(ids, key, PageQuery{PageSize: 3, Cursor: bad}); err == nil {
			t.Errorf("the cursor %q was accepted", bad)
		} else if p, ok := err.(*Problem); !ok || p.Code != CodeCursor {
			t.Errorf("the cursor %q: %v", bad, err)
		}
	}
}

// Prefer is read as RFC 7240 says: preferences separated by commas, each with optional parameters, the
// name without case; only return=preview asks for a preview.
// req: FR-INT-14
func TestPreferReturnPreview(t *testing.T) {
	for prefer, want := range map[string]bool{
		"return=preview":                         true,
		`RETURN = "preview"`:                     true,
		"respond-async, return=preview; foo=bar": true,
		"":                                       false,
		"return=minimal":                         false,
		"return=representation, handling=lenient": false,
		"preview":         false,
		"return=previews": false,
	} {
		if got := wantsPreview(prefer); got != want {
			t.Errorf("wantsPreview(%q) = %v", prefer, got)
		}
	}
}
