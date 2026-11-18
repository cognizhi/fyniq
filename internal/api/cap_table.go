package api

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// ------------------------------------------------------------------------------------------ the cap table (FR-EQ-20)

// CapFigures are a line's or a holder's shares, votes and fully diluted shares, with their percentages of the day's
// totals: exact, shown to 2 decimals half-up (RND-DISPLAY), and never forced to sum to 100.00.
type CapFigures struct {
	Issued       string `json:"issued" pattern:"^(0|[1-9][0-9]*)$" doc:"Shares issued and held, a whole number." example:"1000000"`
	Votes        string `json:"votes" pattern:"^(0|[1-9][0-9]*)(\\.[0-9]+)?$" doc:"Their votes: the shares times their class's votes per share, exact." example:"1000000"`
	FullyDiluted string `json:"fully_diluted" pattern:"^(0|[1-9][0-9]*)$" doc:"Shares fully diluted: the shares issued and the options granted over them (FR-EQ-19). SAFEs and notes count once they convert (FR-EQ-18)." example:"1000000"`
	Options      string `json:"options,omitempty" pattern:"^[1-9][0-9]*$" doc:"The options recorded grants gave over the class by the day, not shares yet (FR-EQ-19); left out when none." example:"48000"`
	Ownership    string `json:"ownership,omitempty" pattern:"^(0|[1-9][0-9]*)\\.[0-9]{2}$" doc:"The percentage of every share issued; left out when none is." example:"79.87"`
	Voting       string `json:"voting,omitempty" pattern:"^(0|[1-9][0-9]*)\\.[0-9]{2}$" doc:"The percentage of every vote; left out when no share votes." example:"79.87"`
	Diluted      string `json:"diluted,omitempty" pattern:"^(0|[1-9][0-9]*)\\.[0-9]{2}$" doc:"The percentage of every share fully diluted; left out when none is." example:"79.87"`
}

// CapTableMovement is a holding movement a cap table figure comes from: the drill-down to its transaction and evidence.
type CapTableMovement struct {
	ID                string `json:"id" format:"uuid" doc:"The movement." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b01"`
	Kind              string `json:"kind" enum:"issue,transfer,class_conversion,reversal" doc:"What moved the shares." example:"issue"`
	Shares            string `json:"shares" pattern:"^-?[1-9][0-9]*$" doc:"Shares in (positive) or out (negative)." example:"200000"`
	EffectiveOn       string `json:"effective_on" format:"date" doc:"The day ACRA's register of members shows it." example:"2027-03-12"`
	TransactionID     string `json:"transaction_id,omitempty" format:"uuid" doc:"The equity transaction it made effective, whose page has its evidence (getEquityTransaction); none for shares recorded before the books start or from founders' money." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5d01"`
	TransactionNumber string `json:"transaction_number,omitempty" doc:"That transaction's number." example:"EQ-0002"`
	ShareIssueID      string `json:"share_issue_id,omitempty" format:"uuid" doc:"The share issue an issue records, in the register of share issues (listShareIssues)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
}

// CapTableLine is one holding on the cap table: a holder's shares of a class.
type CapTableLine struct {
	HolderID    string `json:"holder_id" format:"uuid" doc:"The holder (getHolder)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName  string `json:"holder_name" doc:"Their name." example:"Seedling Ventures Pte. Ltd."`
	ClassID     string `json:"class_id,omitempty" format:"uuid" doc:"The share class the shares count in (getShareClass); none while their account has no recorded class." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	ClassName   string `json:"class_name" doc:"The class's name." example:"Ordinary shares"`
	AccountCode string `json:"account_code" doc:"The class's share-capital account." example:"3010"`
	CapFigures
	VotesAssumed bool               `json:"votes_assumed,omitempty" doc:"The votes count one a share because the account has no recorded class yet: record the class and its rights (FR-EQ-16)." example:"false"`
	Movements    []CapTableMovement `json:"movements" nullable:"false" doc:"The movements up to the day, oldest first."`
}

// CapTableHolder is a holder's lines together, with the percentages of their exact sums.
type CapTableHolder struct {
	HolderID   string `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName string `json:"holder_name" doc:"Their name." example:"Seedling Ventures Pte. Ltd."`
	CapFigures
	Mine bool `json:"mine,omitempty" doc:"The caller's own holding: the holder they sign in as (linkContactMember)." example:"false"`
}

// CapTableAt is the cap table at the end of a day.
type CapTableAt struct {
	At           string           `json:"at" format:"date" doc:"The day, at its end." example:"2027-03-12"`
	Issued       string           `json:"issued" pattern:"^(0|[1-9][0-9]*)$" doc:"Every share issued and held." example:"1252083"`
	Votes        string           `json:"votes" pattern:"^(0|[1-9][0-9]*)(\\.[0-9]+)?$" doc:"Every vote." example:"1252083"`
	FullyDiluted string           `json:"fully_diluted" pattern:"^(0|[1-9][0-9]*)$" doc:"Every share fully diluted: the shares issued, the options granted and what the option pool leaves to grant." example:"1252083"`
	Options      string           `json:"options" pattern:"^(0|[1-9][0-9]*)$" doc:"The options the recorded grants made by the day granted, each counted in its holder's line (FR-EQ-19)." example:"48000"`
	Unallocated  string           `json:"unallocated_pool" pattern:"^-?(0|[1-9][0-9]*)$" doc:"What the option pool in force leaves to grant (FR-EQ-19): its size less the options granted, counted in fully_diluted." example:"52000"`
	PoolDiluted  string           `json:"pool_diluted,omitempty" pattern:"^(0|[1-9][0-9]*)\\.[0-9]{2}$" doc:"The unallocated pool's percentage of every share fully diluted; left out when the pool leaves nothing." example:"4.15"`
	Lines        []CapTableLine   `json:"lines" nullable:"false" doc:"Every holding with shares or options, by holder's name, then class."`
	Holders      []CapTableHolder `json:"holders" nullable:"false" doc:"Every holder with shares or options, by name."`
}

// CapTableView is the cap table at a day, and at a second one to compare.
type CapTableView struct {
	CapTableAt
	Compared *CapTableAt `json:"compared,omitempty" doc:"The cap table at compare, to show beside it."`
}

// MyHolding is the caller's own holding (Appendix D: the Board's "own holding").
type MyHolding struct {
	At           string          `json:"at" format:"date" doc:"The day, at its end." example:"2027-03-12"`
	Linked       bool            `json:"linked" doc:"Whether the caller signs in as a holder (linkContactMember). When false, nothing is held." example:"true"`
	Holder       *CapTableHolder `json:"holder,omitempty" doc:"Their holding, when they hold shares."`
	Lines        []CapTableLine  `json:"lines" nullable:"false" doc:"Their holdings by class."`
	Issued       string          `json:"issued" pattern:"^(0|[1-9][0-9]*)$" doc:"Every share the company has issued, which the percentages are of." example:"1252083"`
	Votes        string          `json:"votes" pattern:"^(0|[1-9][0-9]*)(\\.[0-9]+)?$" doc:"Every vote." example:"1252083"`
	FullyDiluted string          `json:"fully_diluted" pattern:"^(0|[1-9][0-9]*)$" doc:"Every share fully diluted." example:"1252083"`
}

type capTableInput struct {
	At      string `query:"at" format:"date" doc:"The day the cap table is at the end of; left out, the organisation's today." example:"2027-03-12"`
	Compare string `query:"compare" format:"date" doc:"A second day, whose cap table is shown beside it in compared." example:"2027-01-31"`
}

type capTableOutput struct{ Body CapTableView }

type capTableExportInput struct {
	At     string `query:"at" format:"date" doc:"The day the cap table is at the end of; left out, the organisation's today." example:"2027-03-12"`
	Format string `query:"format" enum:"csv,xlsx,pdf" default:"xlsx" doc:"csv: a row per holding, plain decimals; xlsx: a workbook of the holdings and the holders, figures as number cells; pdf: the cap table to print, archival (PDF/A-2a) and accessible (PDF/UA-1)." example:"xlsx"`
}

type myHoldingInput struct {
	At string `query:"at" format:"date" doc:"The day, at its end; left out, the organisation's today." example:"2027-03-12"`
}

type myHoldingOutput struct{ Body MyHolding }

// capView is what showing a cap table reads once: names, account codes, classes, transaction numbers and the caller's
// own holder.
type capView struct {
	names   map[uuid.UUID]string
	codes   map[uuid.UUID]string
	classes map[uuid.UUID][]equity.Recorded[equity.ShareClass]
	numbers map[uuid.UUID]string
	mine    uuid.UUID
	linked  bool
}

func (a *API) capViewOf(ctx context.Context, u storage.UnitOfWork) (capView, error) {
	v := capView{names: map[uuid.UUID]string{}, codes: map[uuid.UUID]string{}, numbers: map[uuid.UUID]string{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.codes[x.ID] = x.Code
	}
	if v.classes, err = equity.ShareClasses(ctx, u); err != nil {
		return v, err
	}
	docs, err := equity.Transactions(ctx, u)
	if err != nil {
		return v, err
	}
	for _, d := range docs {
		v.numbers[d.ID] = d.Number
	}
	if p, _ := authn.From(ctx); p.UserID != uuid.Nil {
		c, err := u.Contacts().OfMember(ctx, p.UserID)
		switch {
		case err == nil:
			v.mine, v.linked = c.ID, true
		case !errors.Is(err, ports.ErrNotFound):
			return v, err
		}
	}
	return v, nil
}

func (v capView) name(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (string, error) {
	if n, ok := v.names[id]; ok {
		return n, nil
	}
	c, err := u.Contacts().Get(ctx, id)
	if err != nil {
		return "", err
	}
	v.names[id] = c.LegalName
	return c.LegalName, nil
}

// classLabel is a holding's class as a person reads it: the recorded class's name, or for shares of an account with
// none, the standard chart's name for it, or its account.
func classLabel(classes map[uuid.UUID][]equity.Recorded[equity.ShareClass], code string, class uuid.UUID) string {
	if cur := equity.InForce(classes[class]); class != uuid.Nil && cur != nil {
		return cur.Value.Name
	}
	if n := map[string]string{"3010": "Ordinary shares", "3020": "Preference shares"}[code]; n != "" {
		return n
	}
	return "Share capital " + code
}

// whole is a decimal as a plain number with no trailing zeros: "1000000", "1.5", "0".
func whole(d apd.Decimal) string {
	var x apd.Decimal
	x.Reduce(&d)
	return x.Text('f')
}

func figures(issued, votes, diluted apd.Decimal, ownership, voting, dilutedShare string) CapFigures {
	out := CapFigures{Issued: whole(issued), Votes: whole(votes), FullyDiluted: whole(diluted), Ownership: ownership, Voting: voting,
		Diluted: dilutedShare}
	var options apd.Decimal
	if _, err := apd.BaseContext.Sub(&options, &diluted, &issued); err == nil && options.Sign() > 0 { // fully diluted is shares and options
		out.Options = whole(options)
	}
	return out
}

// capTableAt is the cap table at the end of day as the API shows it (equity.TableIn), names and classes resolved.
func (a *API) capTableAt(ctx context.Context, u storage.UnitOfWork, v capView, day time.Time) (CapTableAt, error) {
	t, err := equity.TableIn(ctx, u, day)
	if err != nil {
		return CapTableAt{}, err
	}
	out := CapTableAt{At: day.Format(time.DateOnly), Issued: whole(t.Issued), Votes: whole(t.Votes), FullyDiluted: whole(t.FullyDiluted),
		Options: whole(t.Options), Unallocated: whole(t.Unallocated), Lines: []CapTableLine{}, Holders: []CapTableHolder{}}
	if t.Unallocated.Sign() > 0 {
		if out.PoolDiluted, err = rounding.Ownership(&t.Unallocated, &t.FullyDiluted); err != nil {
			return CapTableAt{}, err
		}
	}
	for _, l := range t.Lines {
		name, err := v.name(ctx, u, l.Key.Holder)
		if err != nil {
			return CapTableAt{}, err
		}
		code := v.codes[l.Key.Account]
		line := CapTableLine{HolderID: l.Key.Holder.String(), HolderName: name, ClassName: classLabel(v.classes, code, l.Key.Class), AccountCode: code,
			CapFigures: figures(l.Issued, l.Votes, l.FullyDiluted, l.Ownership, l.Voting, l.Diluted), VotesAssumed: l.VotesAssumed,
			Movements: []CapTableMovement{}}
		if l.Key.Class != uuid.Nil {
			line.ClassID = l.Key.Class.String()
		}
		for _, m := range l.Moves {
			mv := CapTableMovement{ID: m.ID.String(), Kind: m.Kind, Shares: whole(m.Shares), EffectiveOn: m.On.Format(time.DateOnly)}
			if m.Transaction != nil {
				mv.TransactionID, mv.TransactionNumber = m.Transaction.String(), v.numbers[*m.Transaction]
			}
			if m.ShareIssue != nil {
				mv.ShareIssueID = m.ShareIssue.String()
			}
			line.Movements = append(line.Movements, mv)
		}
		out.Lines = append(out.Lines, line)
	}
	for _, h := range t.Holders {
		name, err := v.name(ctx, u, h.Holder)
		if err != nil {
			return CapTableAt{}, err
		}
		out.Holders = append(out.Holders, CapTableHolder{HolderID: h.Holder.String(), HolderName: name, Mine: v.linked && h.Holder == v.mine,
			CapFigures: figures(h.Issued, h.Votes, h.FullyDiluted, h.Ownership, h.Voting, h.Diluted)})
	}
	slices.SortStableFunc(out.Lines, func(x, y CapTableLine) int {
		return cmp.Or(strings.Compare(strings.ToLower(x.HolderName), strings.ToLower(y.HolderName)), strings.Compare(x.HolderID, y.HolderID),
			strings.Compare(x.AccountCode, y.AccountCode), strings.Compare(x.ClassName, y.ClassName))
	})
	slices.SortStableFunc(out.Holders, func(x, y CapTableHolder) int {
		return cmp.Or(strings.Compare(strings.ToLower(x.HolderName), strings.ToLower(y.HolderName)), strings.Compare(x.HolderID, y.HolderID))
	})
	return out, nil
}

// capDay is the day a query names, or the organisation's today when it names none.
func (a *API) capDay(ctx context.Context, u storage.UnitOfWork, field, s string) (time.Time, error) {
	if s == "" {
		return orgToday(ctx, a, u)
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query." + field, Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"}})
	}
	return d, nil
}

// grouped is a whole number with its thousands separated, as a printed cap table shows it.
func grouped(s string) string {
	head, tail, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range head {
		if i > 0 && (len(head)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if tail != "" {
		return b.String() + "." + tail
	}
	return b.String()
}

func percentText(p string) string {
	if p == "" {
		return "–"
	}
	return p + "%"
}

// capTotal is the cap table's total row: every figure, at 100.00% of itself.
func capTotal(t CapTableAt) CapFigures {
	total := CapFigures{Issued: t.Issued, Votes: t.Votes, FullyDiluted: t.FullyDiluted}
	if t.Options != "0" {
		total.Options = t.Options
	}
	if t.Issued != "0" {
		total.Ownership = "100.00"
	}
	if t.FullyDiluted != "0" {
		total.Diluted = "100.00"
	}
	if t.Votes != "0" {
		total.Voting = "100.00"
	}
	return total
}

// poolFigures is the unallocated option pool's row: no shares and no votes, only what it leaves to grant fully diluted.
func poolFigures(t CapTableAt) (CapFigures, bool) {
	if t.PoolDiluted == "" {
		return CapFigures{}, false
	}
	return CapFigures{Issued: "0", Votes: "0", FullyDiluted: t.Unallocated, Diluted: t.PoolDiluted}, true
}

// capSheets are the cap table's workbook: a row per holding, then a row per holder, each with a total.
func capSheets(t CapTableAt) ([]sheet.Sheet, int) {
	decimals := func(s string) int {
		if _, tail, ok := strings.Cut(s, "."); ok {
			return len(tail)
		}
		return 0
	}
	pct := func(p string) sheet.Cell {
		if p == "" {
			return sheet.Text("")
		}
		return sheet.Number(p, 2)
	}
	fig := func(f CapFigures) []sheet.Cell {
		return []sheet.Cell{sheet.Number(f.Issued, 0), pct(f.Ownership), sheet.Number(f.Votes, decimals(f.Votes)), pct(f.Voting),
			sheet.Number(f.FullyDiluted, 0), pct(f.Diluted)}
	}
	total := capTotal(t)
	figureHeads := []string{"Shares issued", "% of shares", "Votes", "% of votes", "Fully diluted", "% fully diluted"}
	lines := sheet.Sheet{Name: "Cap table", Header: append([]string{"Holder", "Class", "Account"}, figureHeads...)}
	for _, l := range t.Lines {
		lines.Rows = append(lines.Rows, sheet.Row{Cells: append([]sheet.Cell{sheet.Text(l.HolderName), sheet.Text(l.ClassName), sheet.Text(l.AccountCode)},
			fig(l.CapFigures)...)})
	}
	pool, hasPool := poolFigures(t)
	if hasPool {
		lines.Rows = append(lines.Rows, sheet.Row{Cells: append([]sheet.Cell{sheet.Text("Unallocated option pool"), sheet.Text(""), sheet.Text("")}, fig(pool)...)})
	}
	lines.Rows = append(lines.Rows, sheet.Row{Strong: true, Cells: append([]sheet.Cell{sheet.Text("Total"), sheet.Text(""), sheet.Text("")}, fig(total)...)})
	holders := sheet.Sheet{Name: "Holders", Header: append([]string{"Holder"}, figureHeads...)}
	for _, h := range t.Holders {
		holders.Rows = append(holders.Rows, sheet.Row{Cells: append([]sheet.Cell{sheet.Text(h.HolderName)}, fig(h.CapFigures)...)})
	}
	if hasPool {
		holders.Rows = append(holders.Rows, sheet.Row{Cells: append([]sheet.Cell{sheet.Text("Unallocated option pool")}, fig(pool)...)})
	}
	holders.Rows = append(holders.Rows, sheet.Row{Strong: true, Cells: append([]sheet.Cell{sheet.Text("Total")}, fig(total)...)})
	return []sheet.Sheet{lines, holders}, len(t.Lines)
}

// capReport is the cap table to print, through the render service's report layout.
func capReport(t CapTableAt, author, version string, revision int64) render.Report {
	r := render.Report{Title: "Cap table", Author: author, Lines: []string{"At " + longDay(t.At),
		"Shares by holder and class, from the register of members; percentages exact and shown to 2 decimals"}, Label: "Holder and class",
		Columns: []string{"Shares issued", "% of shares", "Votes", "% of votes", "Fully diluted", "% fully diluted"}, Rows: []render.ReportRow{},
		Footer: "Cap table · read at ledger revision " + strconv.FormatInt(revision, 10) + " · Fyniq " + version}
	cells := func(f CapFigures) []string {
		return []string{grouped(f.Issued), percentText(f.Ownership), grouped(f.Votes), percentText(f.Voting), grouped(f.FullyDiluted), percentText(f.Diluted)}
	}
	for _, h := range t.Holders {
		var theirs []CapTableLine
		for _, l := range t.Lines {
			if l.HolderID == h.HolderID {
				theirs = append(theirs, l)
			}
		}
		if len(theirs) == 1 {
			r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowLine, Code: theirs[0].AccountCode, Label: h.HolderName + ", " + theirs[0].ClassName,
				Cells: cells(theirs[0].CapFigures)})
			continue
		}
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowSection, Label: h.HolderName})
		for _, l := range theirs {
			r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowLine, Code: l.AccountCode, Label: l.ClassName, Cells: cells(l.CapFigures)})
		}
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowSubtotal, Label: h.HolderName + ", every class", Cells: cells(h.CapFigures)})
	}
	total := capTotal(t)
	if pool, ok := poolFigures(t); ok {
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowLine, Label: "Unallocated option pool", Cells: cells(pool)})
	}
	r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowTotal, Label: "Total", Cells: cells(total)})
	r.Notes = []render.ReportNote{{Title: "How it is read", Items: []string{
		"Each percentage is of the day's total, computed exactly and shown to 2 decimals; they are not forced to sum to 100.00.",
		"Fully diluted counts the shares issued, the options granted and what the option pool leaves to grant; SAFEs and notes count once they convert."}}}
	return r
}

func registerCapTable(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetCapTable, huma.Operation{
		OperationID: "getCapTable", Method: http.MethodGet, Path: "/v1/cap-table", Tags: []string{"Equity"},
		Summary: "Get the cap table",
		Description: "The cap table at the end of a day, computed from the register's holding movements (FR-EQ-20). For each holder and " +
			"class it gives the shares issued, their votes and the shares fully diluted, each with its percentage of the day's total. " +
			"Fully diluted counts each holder's shares and the options their recorded grants gave over the class (FR-EQ-19), and the " +
			"day's total adds what the option pool in force leaves to grant (unallocated_pool, with its percentage pool_diluted); a " +
			"holder with options and no shares has a line of options only. SAFEs and notes count once they convert. Percentages are exact and shown to 2 decimals half-up (RND-DISPLAY), never forced to sum to 100.00. Each holder's figures " +
			"are taken from their exact sums. Each line lists the movements behind it: the equity transaction, whose page has its " +
			"evidence, or the share issue. compare adds the cap table at a second day, to show beside it. The table is never written, " +
			"so no operation edits it: shares move only through equity transactions. Read-only; no side effects.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *capTableInput) (*capTableOutput, error) {
		p, _ := authn.From(ctx)
		var out capTableOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			day, err := a.capDay(ctx, u, "at", in.At)
			if err != nil {
				return err
			}
			v, err := a.capViewOf(ctx, u)
			if err != nil {
				return err
			}
			if out.Body.CapTableAt, err = a.capTableAt(ctx, u, v, day); err != nil {
				return err
			}
			if in.Compare != "" {
				other, err := a.capDay(ctx, u, "compare", in.Compare)
				if err != nil {
					return err
				}
				t, err := a.capTableAt(ctx, u, v, other)
				if err != nil {
					return err
				}
				out.Body.Compared = &t
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.ExportCapTable, huma.Operation{
		OperationID: "exportCapTable", Method: http.MethodGet, Path: "/v1/cap-table/export", Tags: []string{"Equity"},
		Summary: "Export the cap table",
		Description: "The cap table getCapTable gives at the same day, as a file (FR-EQ-20). CSV gives a row per holding, with plain " +
			"decimals. XLSX gives a workbook of the holdings and the holders with their totals, figures as number cells. PDF gives the " +
			"cap table to print from the render service, archival and accessible. No text in any becomes a formula. The file is " +
			"returned in base64 with its SHA-256 and the ledger revision at the time. A PDF the render service cannot make now is " +
			"REPORT-08. Side effects: an audit event of the export, with the day, the format, the rows and the hash. Never cached.",
	}, append([]string{CodeInvalid, CodeReportRender}, locks...), func(ctx context.Context, in *capTableExportInput) (*reportFileOutput, error) {
		p, _ := authn.From(ctx)
		var t CapTableAt
		var revision int64
		author := ""
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			day, err := a.capDay(ctx, u, "at", in.At)
			if err != nil {
				return err
			}
			v, err := a.capViewOf(ctx, u)
			if err != nil {
				return err
			}
			if t, err = a.capTableAt(ctx, u, v, day); err != nil {
				return err
			}
			if revision, err = u.Ledger().Revision(ctx); err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			author = cmpOr(o.Profile.LegalName, o.Name)
			return err
		})
		if err != nil {
			return nil, err
		}
		// The file is written outside the transaction, so the register is not held while the render service lays a PDF out.
		now := a.deps.Clock.Now()
		sheets, rows := capSheets(t)
		name := "cap-table-" + t.At
		var data []byte
		media := mediaXLSX
		switch in.Format {
		case "csv":
			data, media, name = sheet.CSV(a.footed(now, sheets[0])[0]), mediaCSV, name+".csv"
		case "pdf":
			if data, err = a.renderReport(ctx, capReport(t, author, a.stamp(), revision), now); err != nil {
				return nil, err
			}
			media, name = mediaPDF, name+".pdf"
		default:
			if data, err = sheet.XLSX(a.footed(now, sheets...), now); err != nil {
				return nil, err
			}
			name += ".xlsx"
		}
		sum := sha256.Sum256(data)
		id := uuid.Must(uuid.NewV7())
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "report.exported", ObjectType: "report", ObjectID: id,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "cap_table", "at": t.At, "format": cmpOr(in.Format, "xlsx"),
					"ledger_revision": revision, "rows": rows, "file": name, "sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		return &reportFileOutput{Body: ReportFile{ExportID: id.String(), FileName: name, MediaType: media, SHA256: hex.EncodeToString(sum[:]),
			Size: len(data), Rows: rows, Revision: revision, Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})

	register(a, authz.GetMyHolding, huma.Operation{
		OperationID: "getMyHolding", Method: http.MethodGet, Path: "/v1/me/holding", Tags: []string{"Equity"},
		Summary: "Get my holding",
		Description: "The caller's own holding at the end of a day: the shares of the holder they sign in as (linkContactMember), by " +
			"class, with their votes and their percentages of the company's totals, as getCapTable gives them (FR-EQ-20). The Board " +
			"reads its own holding this way (Appendix D). A caller who signs in as no holder gets linked false, with nothing held. " +
			"Read-only; no side effects.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *myHoldingInput) (*myHoldingOutput, error) {
		p, _ := authn.From(ctx)
		var out myHoldingOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			day, err := a.capDay(ctx, u, "at", in.At)
			if err != nil {
				return err
			}
			v, err := a.capViewOf(ctx, u)
			if err != nil {
				return err
			}
			t, err := a.capTableAt(ctx, u, v, day)
			if err != nil {
				return err
			}
			out.Body = MyHolding{At: t.At, Linked: v.linked, Lines: []CapTableLine{}, Issued: t.Issued, Votes: t.Votes, FullyDiluted: t.FullyDiluted}
			if !v.linked {
				return nil
			}
			for _, l := range t.Lines {
				if l.HolderID == v.mine.String() {
					out.Body.Lines = append(out.Body.Lines, l)
				}
			}
			for i := range t.Holders {
				if t.Holders[i].Mine {
					out.Body.Holder = &t.Holders[i]
				}
			}
			return nil
		})
		return &out, err
	})
}
