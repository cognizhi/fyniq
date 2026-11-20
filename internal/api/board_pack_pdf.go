package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// The board pack as a PDF (FR-RPT-06; p2-reporting-planning design D4): a published pack's frozen figures and texts laid out by the render service, the same file each time it is
// asked for, with its SHA-256 recorded in the audit trail. A draft has no PDF: its figures are a preview.

type boardPackPDFInput struct{ BoardPackPath }

// chunkText cuts text into paragraphs of at most 480 characters, at a space where it can (the render service's text bound is 500).
func chunkText(s string) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		para = strings.TrimSpace(para)
		for utf8.RuneCountInString(para) > 480 {
			cut := 480
			rs := []rune(para)
			for i := 480; i > 240; i-- {
				if rs[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, strings.TrimSpace(string(rs[:cut])))
			para = strings.TrimSpace(string(rs[cut:]))
		}
		if para != "" {
			out = append(out, para)
		}
	}
	return out
}

// boardPackReport is a published pack as one report: the month's statements, each beside the period before, then the KPIs, the texts a person wrote, and what the figures were read at.
func boardPackReport(p BoardPack, author, version string) (render.Report, int, error) {
	r := render.Report{Title: "Board pack " + p.Month, Author: author, Label: "Statements", Columns: []string{"This period", "The period before"}, Rows: []render.ReportRow{}}
	var figs struct {
		PL   *FinancialStatement `json:"pl"`
		BS   *FinancialStatement `json:"bs"`
		CF   *FinancialStatement `json:"cf"`
		KPIs *KPIs               `json:"kpis"`
	}
	raw, err := json.Marshal(p.Figures)
	if err != nil {
		return r, 0, err
	}
	if err := json.Unmarshal(raw, &figs); err != nil {
		return r, 0, err
	}
	rows := 0
	for _, s := range []*FinancialStatement{figs.PL, figs.BS, figs.CF} {
		if s == nil {
			continue
		}
		rep := statementReport(*s, author, version)
		if r.Lines == nil {
			r.Lines = []string{"For " + p.Month + ", version " + strconv.Itoa(p.Version), "Amounts in " + s.Currency}
		}
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowSection, Label: statementNames[s.Kind]})
		for _, x := range rep.Rows {
			if x.Kind == render.RowSection {
				x.Cells = nil
			} else {
				// the amounts of the statement's first two columns (the period and the one before), without the percentages of revenue
				step := len(x.Cells) / max(1, len(s.Columns))
				cells := []string{"", ""}
				for i := 0; i < 2 && i*step < len(x.Cells); i++ {
					cells[i] = x.Cells[i*step]
				}
				x.Cells = cells
				rows++
			}
			r.Rows = append(r.Rows, x)
		}
	}
	if r.Lines == nil {
		r.Lines = []string{"For " + p.Month + ", version " + strconv.Itoa(p.Version)}
	}
	if figs.KPIs != nil {
		n := render.ReportNote{Title: "Key figures"}
		for _, k := range figs.KPIs.KPIs {
			v := cmpOr(k.Month.Value, cmpOr(k.Month.Words, cmpOr(k.Month.Undefined, "not available")))
			n.Items = append(n.Items, k.Name+": "+v)
		}
		r.Notes = append(r.Notes, n)
	}
	for _, t := range []struct{ title, text string }{{"Highlights", p.Highlights}, {"Commentary", p.Commentary}, {"What the board is asked for", p.Asks}} {
		if items := chunkText(t.text); len(items) > 0 {
			r.Notes = append(r.Notes, render.ReportNote{Title: t.title, Items: items})
		}
	}
	basis := "Read at ledger revision " + strconv.FormatInt(p.Revision, 10)
	if p.PeriodState != "" && p.PeriodState != "closed" {
		r.Notes = append(r.Notes, render.ReportNote{Title: "The month is not closed", Items: []string{"These figures were read while the month was " + strings.ReplaceAll(p.PeriodState, "_", " ") + ", so they may change when it closes."}})
		basis += " · month not closed"
	}
	r.Footer = "Board pack " + p.Month + " · " + basis + " · Fyniq " + version
	return r, rows, nil
}

// renderedPack is a published pack as the PDF the render service lays out for it.
type renderedPack struct {
	pack BoardPack
	data []byte
	sum  [32]byte
	name string
	rows int
}

// renderPublishedPack reads a published pack and lays its PDF out: the same bytes each time (the creation time is the pack's own publication time). A draft, and a pack that is not
// there, are CodeNotFound.
func (a *API) renderPublishedPack(ctx context.Context, p authn.Principal, packID string) (renderedPack, error) {
	var f renderedPack
	author := ""
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		id, err := parseID("id", packID)
		if err != nil {
			return err
		}
		b, err := u.BoardPacks().Get(ctx, id)
		if errors.Is(err, ports.ErrNotFound) || err == nil && b.Status != storage.PackPublished {
			return problem(CodeNotFound, "No published board pack with that ID is open to you.", "List the board packs")
		}
		if err != nil {
			return err
		}
		f.pack = boardPackOf(b, true)
		o, err := u.Organisation(ctx)
		author = cmpOr(o.Profile.LegalName, o.Name)
		return err
	})
	if err != nil {
		return f, err
	}
	rep, rows, err := boardPackReport(f.pack, author, a.stamp())
	if err != nil {
		return f, err
	}
	// Written outside the transaction, so the register is not held while the render service lays the PDF out.
	created := a.deps.Clock.Now()
	if f.pack.PublishedAt != nil {
		created = f.pack.PublishedAt.UTC()
	}
	if f.data, err = a.renderReport(ctx, rep, created); err != nil {
		return f, err
	}
	f.sum = sha256.Sum256(f.data)
	f.rows = rows
	f.name = "board-pack-" + f.pack.Month + "-v" + strconv.Itoa(f.pack.Version) + ".pdf"
	return f, nil
}

func registerBoardPackPDF(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ExportBoardPack, huma.Operation{
		OperationID: "exportBoardPack", Method: http.MethodGet, Path: "/v1/board-packs/{id}/pdf", Tags: []string{"Reports"},
		Summary: "Export a published board pack as a PDF",
		Description: "A published board pack laid out as a PDF by the render service (FR-RPT-06): the month's statements, each beside the period before, its key figures and the texts a person " +
			"wrote, as frozen when it was published, so the file a board reads is the file it keeps reading; a month that was not closed says so on the page. The file is returned in base64 " +
			"with its SHA-256 and the ledger revision it was read at. A draft has no PDF (BPACK-02 is for a draft changed; here, not found). A PDF the render service cannot make now is " +
			"REPORT-08. Side effects: an audit event of the export with the hash. Never cached.",
	}, append([]string{CodeNotFound, CodeInvalid, CodeReportRender}, locks...), func(ctx context.Context, in *boardPackPDFInput) (*reportFileOutput, error) {
		p, _ := authn.From(ctx)
		f, err := a.renderPublishedPack(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		pack, data, sum, name, rows := f.pack, f.data, f.sum, f.name, f.rows
		export := uuid.Must(uuid.NewV7())
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "report.exported", ObjectType: "report", ObjectID: export,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "board_pack", "pack": pack.ID, "month": pack.Month, "version": pack.Version, "format": "pdf",
					"ledger_revision": pack.Revision, "rows": rows, "file": name, "sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		return &reportFileOutput{Body: ReportFile{ExportID: export.String(), FileName: name, MediaType: mediaPDF, SHA256: hex.EncodeToString(sum[:]), Size: len(data), Rows: rows,
			Revision: pack.Revision, Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})
}
