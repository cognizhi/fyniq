package pack_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

const gst = "sg-gst/2026.1/"

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := pack.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// fixture copies the shipped sg-gst@2026.1 into a file system of its own and applies edits: a file
// mapped to "" is removed; otherwise the edit is a replacement "old→new" of the file's text, or the
// whole new text when it holds no arrow.
func fixture(t *testing.T, edits map[string]string) fstest.MapFS {
	t.Helper()
	return fixtureOf(t, "sg-gst/2026.1", edits)
}

// fixtureOf is fixture for the shipped version in dir.
func fixtureOf(t *testing.T, dir string, edits map[string]string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	err := fs.WalkDir(packs.FS, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(packs.FS, p)
		m[p] = &fstest.MapFile{Data: b}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range edits {
		switch {
		case edit == "":
			delete(m, name)
		case strings.Contains(edit, "→"):
			parts := strings.SplitN(edit, "→", 2)
			f, ok := m[name]
			if !ok || !strings.Contains(string(f.Data), parts[0]) {
				t.Fatalf("%s has no %q to replace", name, parts[0])
			}
			m[name] = &fstest.MapFile{Data: []byte(strings.Replace(string(f.Data), parts[0], parts[1], 1))}
		default:
			m[name] = &fstest.MapFile{Data: []byte(edit)}
		}
	}
	return m
}

// req: ENG-30
// req: FR-ORG-05
// req: ENG-32
// req: platform/release-and-versioning Statutory parameter pack versions
// Every shipped pack version loads, cites its sources, and passes its pinned statutory tests.
func TestTheShippedPacksLoadAndPassTheirPinnedTests(t *testing.T) {
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 || versions[0].ID != "sg-gst@2026.1" {
		t.Fatalf("loaded %v; want sg-gst@2026.1 first", versions)
	}
	for _, v := range versions {
		results := pack.RunPinned(v)
		if len(results) == 0 {
			t.Errorf("%s has no pinned tests", v.ID)
		}
		for _, r := range pack.Failed(results) {
			t.Errorf("%s pinned %s %s (%s): got %s, want %s", v.ID, r.Table, r.Case, r.Why, r.Got, r.Want)
		}
		if len(v.Digest) != 64 {
			t.Errorf("%s has digest %q", v.ID, v.Digest)
		}
	}
}

// req: ENG-30
// req: FR-ORG-05
// req: platform/release-and-versioning Statutory parameter pack versions
// A version without a source URL, a verifier or a review date, or with a value still marked [verify], is
// refused, as is one whose tables break the pack's rules.
func TestAnIncompleteOrUnverifiedVersionIsRefused(t *testing.T) {
	cases := map[string]struct {
		edits map[string]string
		code  string
	}{
		"no source URL":                {map[string]string{gst + "pack.toml": `url = "https://www.iras.gov.sg/taxes/goods-services-tax-(gst)/basics-of-gst/current-gst-rates"→url = ""`}, pack.CodeSource},
		"an http URL":                  {map[string]string{gst + "pack.toml": `url = "https://www.iras.gov.sg/taxes/goods-services-tax-(gst)/basics-of-gst/current-gst-rates"→url = "http://www.iras.gov.sg/"`}, pack.CodeSource},
		"no verifier":                  {map[string]string{gst + "pack.toml": `verifier = "Claude Code→verifier = "  "` + "\n" + `# was "Claude Code`}, pack.CodeVerifier},
		"no review date":               {map[string]string{gst + "pack.toml": `review_date = "2027-01-31"→# no review date`}, pack.CodeReview},
		"a [verify] rate":              {map[string]string{gst + "rates.csv": "2024-01-01,,0.09,→2024-01-01,,0.09 [verify],"}, pack.CodeVerify},
		"a [Verify] note in pack.toml": {map[string]string{gst + "pack.toml": `section = "Box 1→section = "[Verify] Box 1`}, pack.CodeVerify},
		"no retrieval date":            {map[string]string{gst + "pack.toml": `retrieved = "2026-10-20"→retrieved = "last week"`}, pack.CodeSource},
		"an unknown kind":              {map[string]string{gst + "pack.toml": `kind = "official guidance"→kind = "blog"`}, pack.CodeSource},
		"an undeclared source":         {map[string]string{gst + "codes.csv": "SR,supply,true,1,6,Standard-rated supply,iras-f5-boxes→SR,supply,true,1,6,Standard-rated supply,a-blog"}, pack.CodeSource},
		"a row without a source":       {map[string]string{gst + "rates.csv": "2003-01-01,2003-12-31,0.04,iras-current-rates→2003-01-01,2003-12-31,0.04,"}, pack.CodeSource},
		"an id not its directory":      {map[string]string{gst + "pack.toml": `id = "sg-gst@2026.1"→id = "sg-gst@2026.2"`}, pack.CodeID},
		"an unknown key":               {map[string]string{gst + "pack.toml": `title = →approved = "me"` + "\n" + `title = `}, pack.CodeTable},
		"a missing table":              {map[string]string{gst + "codes.csv": ""}, pack.CodeTable},
		"a missing pinned file":        {map[string]string{gst + "pinned/rates.csv": ""}, pack.CodeTable},
		"wrong columns":                {map[string]string{gst + "rates.csv": "effective_from,effective_to,rate,source→from,to,rate,source"}, pack.CodeTable},
		"a percentage":                 {map[string]string{gst + "rates.csv": "2024-01-01,,0.09,→2024-01-01,,9%,"}, pack.CodeValue},
		"a rate of 1 or more":          {map[string]string{gst + "rates.csv": "2024-01-01,,0.09,→2024-01-01,,1.09,"}, pack.CodeValue},
		"NaN":                          {map[string]string{gst + "rates.csv": "2024-01-01,,0.09,→2024-01-01,,NaN,"}, pack.CodeValue},
		"a gap between rates":          {map[string]string{gst + "rates.csv": "2004-01-01,2007-06-30→2004-01-01,2007-05-31"}, pack.CodeDates},
		"an open rate not last":        {map[string]string{gst + "rates.csv": "2023-01-01,2023-12-31→2023-01-01,"}, pack.CodeDates},
		"a repeated code":              {map[string]string{gst + "codes.csv": "ZR,supply→SR,supply"}, pack.CodeValue},
		"a box past 15":                {map[string]string{gst + "codes.csv": "ZR,supply,false,2,→ZR,supply,false,16,"}, pack.CodeValue},
		"an unknown tax point rule":    {map[string]string{gst + "pack.toml": `tax_point_rule = "sg-gst/earlier-of@1"→tax_point_rule = "sg-gst/invoice-date@1"`}, pack.CodeRule},
		"no effective date":            {map[string]string{gst + "pack.toml": `effective_from = "1994-04-01"→effective_from = "soon"`}, pack.CodeDates},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pack.Load(fixture(t, c.edits), "sg-gst/2026.1")
			ps, ok := err.(pack.Problems)
			if !ok {
				t.Fatalf("got %v; want the version refused with %s", err, c.code)
			}
			for _, p := range ps {
				if p.Code == c.code {
					return
				}
			}
			t.Fatalf("got %v; want %s among the problems", ps, c.code)
		})
	}
	// LoadAll refuses the whole set when one version is refused.
	if _, err := pack.LoadAll(fixture(t, map[string]string{gst + "rates.csv": "2024-01-01,,0.09,→2024-01-01,,0.09 [verify],"})); err == nil {
		t.Fatal("LoadAll accepted a set holding a [verify] value")
	}
}

// withFuture returns sg-gst@2026.1 plus sg-gst@2027.1, a made-up future version, made from the shipped 2026.2, with a
// rate of 10% from 1 Jan 2027.
func withFuture(t *testing.T) []pack.Version {
	t.Helper()
	m := fixture(t, nil)
	for name, f := range fixtureOf(t, "sg-gst/2026.2", nil) {
		next := strings.Replace(name, "2026.2", "2027.1", 1)
		m[next] = f
	}
	m["sg-gst/2027.1/pack.toml"] = &fstest.MapFile{Data: []byte(strings.NewReplacer(
		`id = "sg-gst@2026.2"`, `id = "sg-gst@2027.1"`, `effective_from = "1994-04-01"`, `effective_from = "2027-01-01"`).
		Replace(string(m["sg-gst/2027.1/pack.toml"].Data)))}
	m["sg-gst/2027.1/rates.csv"] = &fstest.MapFile{Data: []byte("effective_from,effective_to,rate,source\n2027-01-01,,0.10,iras-current-rates\n")}
	m["sg-gst/2027.1/pinned/rates.csv"] = &fstest.MapFile{Data: []byte("case,date,want_rate,why,source\nX-1,2027-01-01,0.10,made up,iras-current-rates\n")}
	m["sg-gst/2027.1/limits.csv"] = &fstest.MapFile{Data: []byte("limit,effective_from,effective_to,amount,currency,source\nsimplified_invoice,2027-01-01,,1000.00,SGD,iras-invoicing\n")}
	m["sg-gst/2027.1/pinned/limits.csv"] = &fstest.MapFile{Data: []byte("case,limit,date,want,why,source\nX-2,simplified_invoice,2027-01-01,1000.00 SGD,made up,iras-invoicing\n")}
	versions, err := pack.LoadAll(m)
	if err != nil {
		t.Fatal(err)
	}
	return versions
}

// req: ENG-31
// The version in force at a document's tax point gives its rate: a version, or a rate, that comes into
// force later never changes an earlier document.
func TestAFutureRateNeverChangesAPastDocument(t *testing.T) {
	versions := withFuture(t)
	for _, c := range []struct{ taxPoint, version, rate string }{
		{"2023-06-30", "sg-gst@2026.1", "0.08"},
		{"2023-12-31", "sg-gst@2026.1", "0.08"},
		{"2024-01-01", "sg-gst@2026.1", "0.09"},
		{"2026-12-31", "sg-gst@2026.1", "0.09"},
		{"2027-01-01", "sg-gst@2027.1", "0.10"},
	} {
		rate, version, err := pack.GSTRate(versions, day(t, c.taxPoint))
		if err != nil || rate != c.rate || version != c.version {
			t.Errorf("tax point %s: got %s from %s (%v); want %s from %s", c.taxPoint, rate, version, err, c.rate, c.version)
		}
	}
	// Selection is a pure function of its inputs: the same versions and date give the same answer
	// whichever order the versions come in.
	reversed := []pack.Version{versions[1], versions[0]}
	if v, _ := pack.At(reversed, "sg-gst", day(t, "2026-06-01")); v.ID != "sg-gst@2026.1" {
		t.Errorf("reversed order chose %s", v.ID)
	}
	// Without the organisation's activation of the earlier version, nothing is in force before 2027.
	if _, _, err := pack.GSTRate(versions[1:], day(t, "2026-06-01")); err == nil || err.(pack.Problems)[0].Code != pack.CodeNone {
		t.Errorf("got %v; want %s", err, pack.CodeNone)
	}
	if _, _, err := pack.GSTRate(versions, day(t, "1994-03-31")); err == nil {
		t.Error("a supply before GST was introduced got a rate")
	}
	// A correction in force from the same day supersedes the version it corrects.
	correction := versions[0]
	correction.ID, correction.N = "sg-gst@2026.2", 2
	if v, _ := pack.At([]pack.Version{versions[0], correction}, "sg-gst", day(t, "2025-01-01")); v.ID != "sg-gst@2026.2" {
		t.Errorf("chose %s; want the correction sg-gst@2026.2", v.ID)
	}
	// A version that has ended is not in force after its last day.
	ended := versions[1]
	last := day(t, "2027-06-30")
	ended.EffectiveTo = &last
	if _, ok := pack.At([]pack.Version{ended}, "sg-gst", day(t, "2027-07-01")); ok {
		t.Error("a version was chosen after its effective_to")
	}
}

// req: FR-AP-20
// req: ENG-30
// sg-gst@2026.2 adds the simplified-invoice limit: S$1,000 from 23 Oct 2026, the day it was read on IRAS's page, and
// none before, as its history is not recorded. The version before it holds no limits and still loads; a later one
// must hold them, and its limits follow the pack's rules.
func TestTheSimplifiedInvoiceLimitIsInTheGSTPackFromItsVersion(t *testing.T) {
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]pack.Version{}
	for _, v := range versions {
		byID[v.ID] = v
	}
	v1, v2 := byID["sg-gst@2026.1"], byID["sg-gst@2026.2"]
	if _, ok := v1.Tables["limits"]; ok || v2.Supersedes != "sg-gst@2026.1" {
		t.Fatalf("2026.1 holds limits, or 2026.2 does not supersede it (%q)", v2.Supersedes)
	}
	for _, c := range []struct {
		v          pack.Version
		on, amount string
	}{
		{v2, "2026-10-22", ""},
		{v2, "2026-10-23", "1000.00"},
		{v2, "2031-01-01", "1000.00"},
		{v1, "2026-10-23", ""},
	} {
		amount, cur, ok := pack.Limit(c.v, pack.LimitSimplifiedInvoice, day(t, c.on))
		if amount != c.amount || ok != (c.amount != "") || (ok && cur != "SGD") {
			t.Errorf("%s on %s: got %q %q %v; want %q SGD", c.v.ID, c.on, amount, cur, ok, c.amount)
		}
	}
	if _, _, ok := pack.Limit(v2, "unknown", day(t, "2027-01-01")); ok {
		t.Error("an unknown limit was found")
	}
	// Bills read the newest version in force: 2026.2 supersedes 2026.1 from the same day.
	if v, _ := pack.At(versions, "sg-gst", day(t, "2026-11-01")); v.ID != "sg-gst@2026.2" {
		t.Errorf("chose %s on 1 Nov 2026; want sg-gst@2026.2", v.ID)
	}

	const v2dir = "sg-gst/2026.2/"
	cases := map[string]struct {
		edits map[string]string
		code  string
	}{
		"no limits":            {map[string]string{v2dir + "limits.csv": ""}, pack.CodeTable},
		"no pinned limits":     {map[string]string{v2dir + "pinned/limits.csv": ""}, pack.CodeTable},
		"an unknown limit":     {map[string]string{v2dir + "limits.csv": "simplified_invoice,→receipt,"}, pack.CodeValue},
		"a negative amount":    {map[string]string{v2dir + "limits.csv": ",1000.00,→,-1000.00,"}, pack.CodeValue},
		"a float":              {map[string]string{v2dir + "limits.csv": ",1000.00,→,1e3,"}, pack.CodeValue},
		"a currency symbol":    {map[string]string{v2dir + "limits.csv": ",SGD,→,S$,"}, pack.CodeValue},
		"a date before":        {map[string]string{v2dir + "limits.csv": "2026-10-23→1990-01-01"}, pack.CodeDates},
		"no date":              {map[string]string{v2dir + "limits.csv": "2026-10-23→today"}, pack.CodeDates},
		"an end before start":  {map[string]string{v2dir + "limits.csv": "2026-10-23,,→2026-10-23,2026-10-01,"}, pack.CodeDates},
		"overlapping periods":  {map[string]string{v2dir + "limits.csv": "iras-input-tax\n→iras-input-tax\nsimplified_invoice,2027-01-01,,2000.00,SGD,iras-invoicing\n"}, pack.CodeDates},
		"a wrong pinned limit": {map[string]string{v2dir + "pinned/limits.csv": ",2026-10-23,1000.00 SGD,→,2026-10-23,100.00 SGD,"}, pack.CodePinned},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := pack.Load(fixtureOf(t, "sg-gst/2026.2", c.edits), "sg-gst/2026.2")
			if c.code == pack.CodePinned {
				if err != nil || len(pack.Failed(pack.RunPinned(v))) != 1 {
					t.Fatalf("got %v and %v; want one failed pinned case", err, pack.Failed(pack.RunPinned(v)))
				}
				return
			}
			ps, ok := err.(pack.Problems)
			if !ok {
				t.Fatalf("got %v; want the version refused with %s", err, c.code)
			}
			for _, p := range ps {
				if p.Code == c.code {
					return
				}
			}
			t.Fatalf("got %v; want %s among the problems", ps, c.code)
		})
	}
	// Consecutive periods are fine: the limit can change.
	if _, err := pack.Load(fixtureOf(t, "sg-gst/2026.2", map[string]string{v2dir + "limits.csv": "2026-10-23,,→2026-10-23,2026-12-31,1000.00,SGD,iras-invoicing\nsimplified_invoice,2027-01-01,,"}), "sg-gst/2026.2"); err != nil {
		t.Errorf("consecutive periods refused: %v", err)
	}
	// The version before the limits holds none: listing one there is refused.
	m := fixture(t, map[string]string{gst + "pack.toml": `[[pinned]]→[[tables]]` + "\n" + `name = "limits"` + "\n" + `file = "limits.csv"` + "\n\n" + `[[pinned]]`})
	m[gst+"limits.csv"] = &fstest.MapFile{Data: []byte("limit,effective_from,effective_to,amount,currency,source\n")}
	if _, err := pack.Load(m, "sg-gst/2026.1"); err == nil || !strings.Contains(err.Error(), "is not a table of this version") {
		t.Errorf("got %v; want the limits of 2026.1 refused", err)
	}
}

func ptr(t time.Time) *time.Time { return &t }

// req: ENG-45
// The tax point is the earlier of the invoice and the payment received; a security deposit counts only
// once applied to the price. G4-018 and G4-019 are the review's pinned cases (Appendix G.4).
func TestTheTaxPointIsTheEarlierOfTheInvoiceAndThePayment(t *testing.T) {
	rule := pack.RuleEarlierOf
	for name, c := range map[string]struct {
		s    pack.Supply
		want string
	}{
		"invoice only":            {pack.Supply{Invoiced: ptr(day(t, "2026-04-10"))}, "2026-04-10"},
		"paid after the invoice":  {pack.Supply{Invoiced: ptr(day(t, "2026-04-10")), Payments: []pack.Payment{{Received: day(t, "2026-05-02")}}}, "2026-04-10"},
		"paid before the invoice": {pack.Supply{Invoiced: ptr(day(t, "2026-04-10")), Payments: []pack.Payment{{Received: day(t, "2026-03-30")}}}, "2026-03-30"},
		"paid, no invoice yet":    {pack.Supply{Payments: []pack.Payment{{Received: day(t, "2026-03-30")}}}, "2026-03-30"},
		"G4-018 part-payment deposit in Q1, invoice in Q2": {pack.Supply{Invoiced: ptr(day(t, "2026-04-15")),
			Payments: []pack.Payment{{Received: day(t, "2026-02-15")}}}, "2026-02-15"},
		"G4-019 security deposit applied three months later": {pack.Supply{Invoiced: ptr(day(t, "2026-04-20")),
			Payments: []pack.Payment{{Received: day(t, "2026-01-10"), SecurityDeposit: true, Applied: ptr(day(t, "2026-04-10"))}}}, "2026-04-10"},
	} {
		got, ok, err := pack.TaxPoint(rule, c.s)
		if err != nil || !ok || got.Format(time.DateOnly) != c.want {
			t.Errorf("%s: got %s (%v, %v); want %s", name, got.Format(time.DateOnly), ok, err, c.want)
		}
	}
	// A security deposit not yet applied is not a payment: no tax point yet (G4-019, on receipt).
	held := pack.Supply{Payments: []pack.Payment{{Received: day(t, "2026-01-10"), SecurityDeposit: true}}}
	if _, ok, err := pack.TaxPoint(rule, held); ok || err != nil {
		t.Errorf("a held security deposit gave a tax point (%v)", err)
	}
	if _, _, err := pack.TaxPoint("sg-gst/invoice-date@1", held); err == nil {
		t.Error("an unknown rule was accepted")
	}
	// The rate then comes from the tax point: G4-018's deposit in 2023 bears 8% though invoiced in 2024.
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	tp, _, _ := pack.TaxPoint(rule, pack.Supply{Invoiced: ptr(day(t, "2024-01-05")), Payments: []pack.Payment{{Received: day(t, "2023-12-20")}}})
	if rate, _, _ := pack.GSTRate(versions, tp); rate != "0.08" {
		t.Errorf("a payment received on 20 Dec 2023 for a supply invoiced in 2024 got %s; want 0.08", rate)
	}
}

// req: ENG-32
// req: SOD-10
// Activation needs every pinned test to pass and a second approver through the approval engine, which
// does not exist yet: every activation is refused until it does.
func TestActivationNeedsPinnedTestsAndASecondApprover(t *testing.T) {
	versions, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	v := versions[0]
	alice, bob := uuid.New(), uuid.New()
	has := func(ps pack.Problems, code string) bool {
		for _, p := range ps {
			if p.Code == code {
				return true
			}
		}
		return false
	}
	if ps := pack.CheckActivation(v, pack.Activation{ProposedBy: alice, ApprovedBy: bob}); !has(ps, pack.CodeApproval) {
		t.Errorf("activation without the approval engine: got %v; want %s", ps, pack.CodeApproval)
	}
	if ps := pack.CheckActivation(v, pack.Activation{ProposedBy: alice, ApprovedBy: alice, Approval: uuid.New()}); !has(ps, pack.CodeApproval) {
		t.Errorf("approved by its proposer: got %v; want %s", ps, pack.CodeApproval)
	}
	if ps := pack.CheckActivation(v, pack.Activation{ProposedBy: alice, ApprovedBy: bob, Approval: uuid.New()}); len(ps) != 0 {
		t.Errorf("a valid activation was refused: %v", ps)
	}
	// A rate that disagrees with its hand-pinned case blocks activation.
	wrong, err := pack.Load(fixture(t, map[string]string{gst + "rates.csv": "2024-01-01,,0.09,→2024-01-01,,0.07,"}), "sg-gst/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	if ps := pack.CheckActivation(wrong, pack.Activation{ProposedBy: alice, ApprovedBy: bob, Approval: uuid.New()}); !has(ps, pack.CodePinned) {
		t.Errorf("a version failing its pinned tests: got %v; want %s", ps, pack.CodePinned)
	}
}
