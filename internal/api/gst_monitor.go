package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/gstmonitor"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the GST registration monitor (FR-TAX-05, C.1 #37)
//
// While the organisation is not registered, the monitor tests whether it must be (p1-gst design D3 and D4): last
// calendar year's taxable supplies and this year's so far against the threshold of the sg-gst pack (the retrospective
// test), and the forecast of the next 12 months a person recorded with its evidence (the prospective test). It warns
// once the year so far reaches the warning percentage, 80 unless changed (the owner's decision of 7 Nov 2026).

// GSTMonitorCodes are the codes a forecast answers besides the common ones.
var GSTMonitorCodes = []string{gstmonitor.CodeForecast, gstmonitor.CodeNoEvidence, gstmonitor.CodeRecorded, storage.CodeGSTForecast}

// GSTForecastDraft is a forecast as drafted.
type GSTForecastDraft struct {
	ForecastOn string `json:"forecast_on" format:"date" doc:"The day the forecast was made: its 12 months run from it. Today or earlier." example:"2026-11-20"`
	Value      Money  `json:"value" doc:"The taxable supplies expected in the 12 months, in Singapore dollars: standard-rated and zero-rated sales, without exempt or out-of-scope sales or sales of the company's own assets."`
	Basis      string `json:"basis" maxLength:"1000" doc:"What the forecast rests on, such as the signed contracts; attach them as evidence." example:"Signed contracts with Lumen Health and Acme Analytics"`
}

// GSTForecastView is a forecast, drafted or recorded.
type GSTForecastView struct {
	ID         string      `json:"id" format:"uuid" doc:"The forecast's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	Status     string      `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its evidence and to be recorded; recorded: the latest recorded; superseded: a later one is recorded." example:"recorded"`
	Version    int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	ForecastOn string      `json:"forecast_on" format:"date" doc:"The day the forecast was made." example:"2026-11-20"`
	Until      string      `json:"until" format:"date" doc:"The last day of its 12 months." example:"2027-11-19"`
	Value      Money       `json:"value" doc:"The taxable supplies expected in the 12 months."`
	Basis      string      `json:"basis" doc:"What it rests on." example:"Signed contracts with Lumen Health and Acme Analytics"`
	Evidence   int         `json:"evidence" doc:"How many files are attached and not removed: recording needs one." example:"1"`
	RecordedBy *Person     `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-11-20T09:30:00Z"`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// GSTForecastPage is a page of forecasts, the latest recorded first, then the drafts.
type GSTForecastPage struct {
	Items      []GSTForecastView `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// GSTYearTest is a calendar year's taxable supplies against the threshold.
type GSTYearTest struct {
	Year       int    `json:"year" doc:"The calendar year." example:"2026"`
	From       string `json:"from" format:"date" doc:"Its first day." example:"2026-01-01"`
	To         string `json:"to" format:"date" doc:"Its last day, or today while it runs." example:"2026-11-20"`
	Supplies   Money  `json:"taxable_supplies" doc:"The taxable supplies posted in it (design D3): the lines of invoices, credit notes and bank transactions coded for Box 1 or Box 2, on accounts that are not assets, in Singapore dollars."`
	Documents  int    `json:"entries" doc:"How many posted entries make the figure; a void counts as its own." example:"14"`
	Unmatched  int    `json:"unmatched" doc:"Documents whose entry's lines did not match their lines, so their codes could not be read: left out, and named on C.1 #37." example:"0"`
	Exceeded   bool   `json:"exceeded" doc:"true when it is more than the threshold." example:"false"`
	ApplyBy    string `json:"apply_by,omitempty" format:"date" doc:"When exceeded: the day to apply to register by (sg-deadlines)." example:"2027-01-30"`
	StartsFrom string `json:"registered_from,omitempty" format:"date" doc:"When exceeded: the day the registration would start (sg-deadlines)." example:"2027-03-01"`
}

// GSTForecastTest is the forecast in force against the threshold.
type GSTForecastTest struct {
	Forecast   GSTForecastView `json:"forecast" doc:"The latest recorded forecast whose 12 months hold today."`
	Exceeded   bool            `json:"exceeded" doc:"true when its value is more than the threshold." example:"false"`
	ApplyBy    string          `json:"apply_by,omitempty" format:"date" doc:"When exceeded: 30 days after the forecast's day (sg-deadlines)." example:"2026-12-20"`
	StartsFrom string          `json:"registered_from,omitempty" format:"date" doc:"When exceeded: 2 months from the forecast's day (sg-deadlines)." example:"2027-01-20"`
}

// GSTMonitor is the registration monitor on a day.
type GSTMonitor struct {
	On           string           `json:"on" format:"date" doc:"The day the tests are read on: today." example:"2026-11-20"`
	Status       string           `json:"status" enum:"registered,liable,near,below,not_assessed" doc:"registered: nothing to test; liable: a test is met, so apply; near: the year so far reached the warning percentage; below: neither; not_assessed: an input is missing, which reason names." example:"below"`
	Reason       string           `json:"reason,omitempty" doc:"Why it is not assessed, or since when the organisation is registered." example:"Registered for GST from 1 Mar 2027"`
	Registration *GSTRegistration `json:"registration,omitempty" doc:"The registration in force, when registered."`
	Threshold    *Money           `json:"threshold,omitempty" doc:"The registration threshold of the sg-gst version in force: more than it makes a business liable."`
	Warning      string           `json:"warning_percent,omitempty" doc:"The warning percentage, the setting gst.threshold_warning_percent." example:"80"`
	Share        string           `json:"share,omitempty" doc:"The year so far as a percentage of the threshold, to one decimal (RND-PERCENT): for showing only; the test compares the exact amounts." example:"12.5"`
	LastYear     *GSTYearTest     `json:"last_year,omitempty" doc:"Last calendar year: the retrospective test."`
	ThisYear     *GSTYearTest     `json:"this_year,omitempty" doc:"This calendar year so far."`
	Forecast     *GSTForecastTest `json:"forecast,omitempty" doc:"The forecast in force: the prospective test; absent while none is recorded for today."`
	Revision     int64            `json:"ledger_revision,omitempty" doc:"The ledger revision the figures were read at (ENG-46)." example:"42"`
}

type (
	GSTForecastPath struct {
		ForecastID string `path:"forecast_id" format:"uuid" doc:"The GST forecast." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	GSTForecastEvidencePath struct {
		GSTForecastPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	gstMonitorOutput      struct{ Body GSTMonitor }
	gstForecastListInput  struct{ PageQuery }
	gstForecastListOutput struct{ Body GSTForecastPage }
	gstForecastDraftInput struct {
		KeyHeader
		PreferHeader
		Body GSTForecastDraft
	}
	gstForecastReviseInput struct {
		GSTForecastPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body GSTForecastDraft
	}
	gstForecastRecordInput struct {
		GSTForecastPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	gstForecastReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body GSTForecastView
	}
	gstForecastWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     GSTForecastView
	}
	addGSTForecastEvidenceInput struct {
		GSTForecastPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listGSTForecastEvidenceInput struct {
		GSTForecastPath
		PageQuery
	}
	removeGSTForecastEvidenceInput struct {
		GSTForecastEvidencePath
		IfMatchHeader
		PreferHeader
	}
)

// forecastIn reads a forecast's fields, or what of them is wrong.
func forecastIn(d GSTForecastDraft) (gstmonitor.Forecast, error) {
	f := gstmonitor.Forecast{Basis: strings.TrimSpace(d.Basis)}
	var fields []FieldProblem
	if day := dayIn("body.forecast_on", d.ForecastOn, &fields); day != nil {
		f.ForecastOn = *day
	}
	if v, fp := moneyIn(d.Value, "body.value"); fp != nil {
		fields = append(fields, *fp)
	} else {
		f.Value = v
	}
	if len(fields) > 0 {
		return f, invalid(fields)
	}
	return f, nil
}

func dateOut(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func (a *API) forecastOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, f gstmonitor.Forecast, latest uuid.UUID) (GSTForecastView, error) {
	out := GSTForecastView{ID: doc.ID.String(), Status: "draft", Version: doc.Version, ForecastOn: f.ForecastOn.Format(time.DateOnly),
		Until: f.Until().Format(time.DateOnly), Value: moneyOut(f.Value), Basis: f.Basis}
	switch {
	case doc.Status == storage.DocumentPosted && doc.ID == latest:
		out.Status = "recorded"
	case doc.Status == storage.DocumentPosted:
		out.Status = "superseded"
	}
	var err error
	out.Evidence, _, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, nil)
	for k := range out.Checks {
		if out.Checks[k].Code != "" {
			out.Checks[k].Code = gstmonitor.CodeNoEvidence
			out.Checks[k].Message = "it has no evidence attached, so it cannot be recorded"
			out.Checks[k].Fix = "Attach what it rests on, such as the signed contracts or confirmed orders"
		}
	}
	return out, err
}

// latestForecast is the ID of the latest recorded forecast, or nil.
func latestForecast(rs []gstmonitor.Recorded) uuid.UUID {
	if len(rs) == 0 {
		return uuid.Nil
	}
	return rs[0].Doc.ID
}

// shownForecast is a forecast by document ID, as the caller reads it: not found for anything else.
func (a *API) shownForecast(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (GSTForecastView, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return GSTForecastView{}, err
	}
	if doc.Type != storage.DocumentGSTForecast {
		return GSTForecastView{}, ports.ErrNotFound
	}
	f, err := gstmonitor.Read(doc)
	if err != nil {
		return GSTForecastView{}, err
	}
	rs, err := gstmonitor.Forecasts(ctx, u)
	if err != nil {
		return GSTForecastView{}, err
	}
	return a.forecastOf(ctx, u, doc, f, latestForecast(rs))
}

func forecastAnswer(w written[GSTForecastView]) *gstForecastWriteOutput {
	if w.Previewed {
		return &gstForecastWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	status := "draft"
	if w.Body.Status != "draft" {
		status = "posted"
	}
	return &gstForecastWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/gst-forecasts/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// monitorOut is the monitor as the API shows it.
func (a *API) monitorOut(ctx context.Context, u storage.UnitOfWork, r gstmonitor.Result) (GSTMonitor, error) {
	out := GSTMonitor{On: r.On.Format(time.DateOnly), Status: string(r.Status), Reason: r.Reason, Warning: r.Warning, Share: r.Share}
	if r.Registration != nil {
		reg := gstOf(*r.Registration)
		out.Registration = &reg
	}
	if !r.Threshold.IsZero() {
		t := moneyOut(r.Threshold)
		out.Threshold = &t
	}
	year := func(t *gstmonitor.YearTest) *GSTYearTest {
		if t == nil {
			return nil
		}
		out.Revision = t.Supplies.Revision
		return &GSTYearTest{Year: t.Year, From: t.Supplies.From.Format(time.DateOnly), To: t.Supplies.To.Format(time.DateOnly), Supplies: moneyOut(t.Supplies.Total),
			Documents: len(t.Supplies.Entries), Unmatched: len(t.Supplies.Unmatched), Exceeded: t.Exceeded, ApplyBy: dateOut(t.ApplyBy), StartsFrom: dateOut(t.StartsOn)}
	}
	out.LastYear, out.ThisYear = year(r.LastYear), year(r.ThisYear)
	if r.Forecast != nil {
		v, err := a.forecastOf(ctx, u, r.Forecast.Doc, r.Forecast.Forecast, latestForecast(r.Forecasts))
		if err != nil {
			return out, err
		}
		out.Forecast = &GSTForecastTest{Forecast: v, Exceeded: r.Forecast.Exceeded, ApplyBy: dateOut(r.Forecast.ApplyBy), StartsFrom: dateOut(r.Forecast.StartsOn)}
	}
	return out, nil
}

func registerGSTMonitor(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.GetGSTMonitor, huma.Operation{
		OperationID: "getGSTMonitor", Method: http.MethodGet, Path: "/v1/compliance/gst-monitor", Tags: []string{"Compliance"},
		Summary: "Get the GST registration monitor",
		Description: "Whether the organisation must register for GST, tested today (FR-TAX-05, design D4): last calendar year's taxable " +
			"supplies and this year's so far against the sg-gst pack's threshold (the retrospective test, IRAS: more than $1 million in " +
			"a calendar year), and the forecast in force (the prospective test: more than $1 million expected in the next 12 months), " +
			"with the days to apply by and to be registered from when a test is met. It warns at the setting " +
			"gst.threshold_warning_percent of the threshold. A registered organisation is not tested. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*gstMonitorOutput, error) {
		p, _ := authn.From(ctx)
		var out gstMonitorOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			r, err := gstmonitor.Monitor(ctx, u, today)
			if err != nil {
				return err
			}
			out.Body, err = a.monitorOut(ctx, u, r)
			return err
		})
		return &out, err
	})

	register(a, authz.ListGSTForecasts, huma.Operation{
		OperationID: "listGSTForecasts", Method: http.MethodGet, Path: "/v1/gst-forecasts", Tags: []string{"Compliance"},
		Summary:     "List the GST forecasts",
		Description: "Every forecast of the taxable supplies of the next 12 months, the latest recorded first, then the drafts, a page at a time (FR-TAX-05). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *gstForecastListInput) (*gstForecastListOutput, error) {
		p, _ := authn.From(ctx)
		out := gstForecastListOutput{Body: GSTForecastPage{Items: []GSTForecastView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := gstmonitor.Forecasts(ctx, u)
			if err != nil {
				return err
			}
			latest := latestForecast(rs)
			var all []GSTForecastView
			for _, r := range rs {
				v, err := a.forecastOf(ctx, u, r.Doc, r.Forecast, latest)
				if err != nil {
					return err
				}
				all = append(all, v)
			}
			drafts, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentGSTForecast, Status: storage.DocumentDraft, Limit: 200})
			if err != nil {
				return err
			}
			for _, doc := range drafts {
				f, err := gstmonitor.Read(doc)
				if err != nil {
					return err
				}
				v, err := a.forecastOf(ctx, u, doc, f, latest)
				if err != nil {
					return err
				}
				all = append(all, v)
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x GSTForecastView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []GSTForecastView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateGSTForecast, huma.Operation{
		OperationID: "createGSTForecast", Method: http.MethodPost, Path: "/v1/gst-forecasts", Tags: []string{"Compliance"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a GST forecast",
		Description: "Drafts a forecast of the taxable supplies of the 12 months from a day, for the prospective test (FR-TAX-05, design D4). " +
			"IRAS asks for supporting documents, such as signed contracts or confirmed orders; a business plan or a sales target is not " +
			"certain enough. Attach them with addGSTForecastEvidence, then record it with recordGSTForecast. Side effects: the draft, " +
			"audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), GSTMonitorCodes...), func(ctx context.Context, in *gstForecastDraftInput) (*gstForecastWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (GSTForecastView, error) {
			f, err := forecastIn(in.Body)
			if err != nil {
				return GSTForecastView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return GSTForecastView{}, err
			}
			doc, err := gstmonitor.DraftIn(ctx, u, f, today)
			if err != nil {
				return GSTForecastView{}, equityProblems(err)
			}
			return a.shownForecast(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return forecastAnswer(w), nil
	})

	register(a, authz.GetGSTForecast, huma.Operation{
		OperationID: "getGSTForecast", Method: http.MethodGet, Path: "/v1/gst-forecasts/{forecast_id}", Tags: []string{"Compliance"},
		Summary:     "Get a GST forecast",
		Description: "A forecast, drafted or recorded, with its status, its files and who recorded it (FR-TAX-05). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *GSTForecastPath) (*gstForecastReadOutput, error) {
		id, err := parseID("forecast_id", in.ForecastID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out gstForecastReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.shownForecast(ctx, u, id)
			status := "draft"
			if v.Status != "draft" {
				status = "posted"
			}
			out.Body, out.ETag = v, etag(v.Version, status)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceGSTForecast, huma.Operation{
		OperationID: "replaceGSTForecast", Method: http.MethodPut, Path: "/v1/gst-forecasts/{forecast_id}", Tags: []string{"Compliance"},
		Summary: "Change a drafted GST forecast",
		Description: "Saves a new version of a forecast not recorded yet (FR-TAX-05); a recorded one changes no more (GST-32): record a new " +
			"one instead. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), GSTMonitorCodes...),
		func(ctx context.Context, in *gstForecastReviseInput) (*gstForecastWriteOutput, error) {
			id, err := parseID("forecast_id", in.ForecastID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GSTForecastView, error) {
				doc, err := gstmonitor.Drafted(ctx, u, id)
				if err != nil {
					return GSTForecastView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return GSTForecastView{}, err
				}
				f, err := forecastIn(in.Body)
				if err != nil {
					return GSTForecastView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return GSTForecastView{}, err
				}
				if _, err := gstmonitor.ReviseIn(ctx, u, id, f, today); err != nil {
					return GSTForecastView{}, equityProblems(err)
				}
				return a.shownForecast(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return forecastAnswer(w), nil
		})

	register(a, authz.RecordGSTForecast, huma.Operation{
		OperationID: "recordGSTForecast", Method: http.MethodPost, Path: "/v1/gst-forecasts/{forecast_id}/record", Tags: []string{"Compliance"},
		Summary: "Record a GST forecast",
		Description: "Records a drafted forecast (FR-TAX-05, design D4): from then on the latest recorded forecast is the prospective test's, " +
			"never changed. It needs its evidence attached (GST-31). It posts nothing. Side effects: the forecast recorded, its files " +
			"committed, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), GSTMonitorCodes...),
		func(ctx context.Context, in *gstForecastRecordInput) (*gstForecastWriteOutput, error) {
			id, err := parseID("forecast_id", in.ForecastID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GSTForecastView, error) {
				doc, err := gstmonitor.Drafted(ctx, u, id)
				if err != nil {
					return GSTForecastView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return GSTForecastView{}, err
				}
				if _, err := gstmonitor.RecordIn(ctx, u, id); err != nil {
					return GSTForecastView{}, equityProblems(err)
				}
				if a.deps.Outbox != nil { // its evidence committed now, not at the next sweep (FR-DOC-13)
					p, _ := authn.From(ctx)
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return GSTForecastView{}, err
					}
				}
				return a.shownForecast(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return forecastAnswer(w), nil
		})

	evidenceLocks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	addCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, equity.CodeRecorded}, append(evidenceLocks, EvidenceCodes...)...)
	removeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, equity.CodeRecorded}, evidenceLocks...)
	register(a, authz.AddGSTForecastEvidence, huma.Operation{
		OperationID: "addGSTForecastEvidence", Method: http.MethodPost, Path: "/v1/gst-forecasts/{forecast_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach what a GST forecast rests on",
		Description: "Attaches a file to a drafted forecast (FR-TAX-05, FR-DOC-05), as multipart/form-data: a signed contract or a " +
			"confirmed order, as IRAS asks. Recording needs one. The file's type is decided from its content; it is scanned for malware " +
			"and its image metadata is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its " +
			"evidence row and its audit event; once recorded its files are committed to the retention-locked store (FR-DOC-13). Needs " +
			"If-Match with the forecast's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, addCodes, func(ctx context.Context, in *addGSTForecastEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentGSTForecast, "/v1/gst-forecasts", in.ForecastID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
	})
	register(a, authz.ListGSTForecastEvidence, huma.Operation{
		OperationID: "listGSTForecastEvidence", Method: http.MethodGet, Path: "/v1/gst-forecasts/{forecast_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a GST forecast's files",
		Description: "The files attached to a forecast, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listGSTForecastEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentGSTForecast, "forecast_id", in.ForecastID, in.PageQuery)
	})
	register(a, authz.GetGSTForecastEvidence, huma.Operation{
		OperationID: "getGSTForecastEvidence", Method: http.MethodGet, Path: "/v1/gst-forecasts/{forecast_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a GST forecast's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *GSTForecastEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentGSTForecast, "forecast_id", in.ForecastID, in.EvidenceID)
	})
	register(a, authz.RemoveGSTForecastEvidence, huma.Operation{
		OperationID: "removeGSTForecastEvidence", Method: http.MethodDelete, Path: "/v1/gst-forecasts/{forecast_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove a file from a drafted GST forecast",
		Description: "Takes a file off a forecast not recorded yet (FR-DOC-07). Once recorded its evidence is append-only, and the database " +
			"refuses. The row stays, marked removed, for the audit trail. Side effects: the removal and its audit event. Needs If-Match " +
			"with the file's ETag; Prefer: return=preview saves nothing.",
	}, removeCodes, func(ctx context.Context, in *removeGSTForecastEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentGSTForecast, "forecast_id", in.ForecastID, in.EvidenceID, in.IfMatch, in.Prefer, in)
	})
	if op := a.OpenAPI().Paths["/v1/gst-forecasts/{forecast_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}
}
