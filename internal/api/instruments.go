package api

import (
	"context"
	"fmt"
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
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------- SAFEs and convertible notes (FR-EQ-18; design D6)

// InstrumentCodes are the instruments' own refusals.
var InstrumentCodes = []string{equity.CodeInstrument, equity.CodeTemplate, equity.CodeRecorded, storage.CodeInstrument}

// InstrumentDraft is an instrument's terms, as drafted.
type InstrumentDraft struct {
	Form             string `json:"form" enum:"post_money_safe,pre_money_safe,discount_safe,mfn_safe,convertible_note" doc:"post_money_safe: a SAFE whose valuation cap counts the company after the SAFEs convert; pre_money_safe: one whose cap counts the shares before the round; discount_safe: a discount on the round price and no cap; mfn_safe: the best terms of a later instrument; convertible_note: a loan that converts, with interest and a maturity." example:"post_money_safe"`
	HolderID         string `json:"holder_id" format:"uuid" doc:"The investor who holds it, a contact marked investor or shareholder. It stays once drafted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Amount           string `json:"amount" pattern:"^[0-9]+(\\.[0-9]{1,2})?$" doc:"What the investor paid, in the base currency." example:"125000.00"`
	SignedOn         string `json:"signed_on" format:"date" doc:"The day both sides signed it." example:"2026-06-18"`
	ValuationCap     string `json:"valuation_cap,omitempty" pattern:"^([0-9]+(\\.[0-9]{1,8})?)?$" doc:"A capped SAFE's valuation cap, in the base currency." example:"2500000"`
	Discount         string `json:"discount,omitempty" pattern:"^([0-9]+(\\.[0-9]{1,8})?)?$" doc:"A percentage off the round price, above 0 and below 100." example:"20"`
	MFN              bool   `json:"mfn,omitempty" doc:"A most-favoured-nation clause." example:"false"`
	InterestRate     string `json:"interest_rate,omitempty" pattern:"^([0-9]+(\\.[0-9]{1,8})?)?$" doc:"A note's simple interest, a percentage a year." example:"8"`
	MaturityOn       string `json:"maturity_on,omitempty" format:"date" doc:"A note's maturity." example:"2028-06-18"`
	Trigger          string `json:"trigger" enum:"priced_round,maturity,liquidity_event,other" doc:"What converts it: the next priced round, its maturity, a liquidity event, or what the terms say." example:"priced_round"`
	ClassAccountID   string `json:"class_account_id" format:"uuid" doc:"The share-capital account of the class it converts into." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ClassID          string `json:"class_id,omitempty" format:"uuid" doc:"The recorded class it converts into; left out, the account's first." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	Denominator      string `json:"denominator" enum:"post_money_capitalisation,pre_money_shares,as_terms" doc:"How its terms count the shares its price divides by: the company after the SAFEs convert, the shares before the round, or as the terms say, quoted in denominator_note." example:"post_money_capitalisation"`
	DenominatorNote  string `json:"denominator_note,omitempty" maxLength:"1000" doc:"With as_terms: how the terms count the shares." example:"All shares, options granted and the unallocated pool, before the round"`
	InterestConverts bool   `json:"interest_converts,omitempty" doc:"A note's interest converts into shares with its principal." example:"false"`
	PriceDecimals    int    `json:"price_decimals" minimum:"0" maximum:"6" doc:"The decimals the terms give the conversion price." example:"4"`
	Classification   string `json:"classification,omitempty" enum:"liability,equity" doc:"The accountant's classification, liability or equity [verify per instrument]; left out until decided." example:"liability"`
	Template         string `json:"template,omitempty" doc:"The template its terms follow (listInstrumentTemplates), when one does." example:"yc-sg-post-money-cap@1"`
	ProRata          bool   `json:"pro_rata,omitempty" doc:"A pro rata side letter." example:"true"`
	Note             string `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Lion Rock Ventures, seed"`
}

// InstrumentView is an instrument as the API shows it.
type InstrumentView struct {
	InstrumentDraft
	ID              string                   `json:"id" format:"uuid" doc:"The instrument." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	Status          string                   `json:"status" enum:"draft,recorded" doc:"draft: waiting for the signed instrument and approval; recorded: in the register." example:"recorded"`
	Version         int                      `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	HolderName      string                   `json:"holder_name" doc:"Its holder's name." example:"Lion Rock Ventures Pte. Ltd."`
	Currency        string                   `json:"currency" doc:"The amount's currency, the base currency." example:"SGD"`
	ClassCode       string                   `json:"class_code" doc:"The class's account code." example:"3010"`
	TemplateEnabled bool                     `json:"template_enabled,omitempty" doc:"Its template is enabled, so it can convert by itself when every term it has is one the template covers." example:"false"`
	Held            *Money                   `json:"held,omitempty" doc:"Once recorded: what 2700 Convertible notes / SAFE holds for it today, from the ledger: the money received under it, less what converted or was repaid (FR-EQ-18). Absent while the chart has no account of role convertible-instruments."`
	Evidence        int                      `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, the signed instrument." example:"1"`
	ApprovalID      string                   `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a11"`
	RecordedBy      *Person                  `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt      *time.Time               `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-06-20T09:30:00Z"`
	Checks          []BillCheck              `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
	Conversion      *InstrumentConversionRef `json:"conversion,omitempty" doc:"Its conversion into shares, once one is drafted from a priced round and while it stands (draftInstrumentConversions)."`
}

// InstrumentConversionRef is an instrument's conversion: the equity transaction and where it stands.
type InstrumentConversionRef struct {
	TransactionID string `json:"transaction_id" format:"uuid" doc:"The instrument_conversion equity transaction." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a31"`
	Number        string `json:"number,omitempty" doc:"Its number, EQ-, once approved." example:"EQ-0004"`
	Status        string `json:"status" enum:"draft,approved,effective" doc:"draft: drafted for approval; approved: waiting for ACRA's date; effective: its shares were allotted." example:"effective"`
}

// ConversionPreview is what a priced round converts (FR-EQ-18, design D6), as a sandbox: nothing posts and nothing is
// saved. Every figure comes from the engine; the working shown never re-rounds what posts.
type ConversionPreview struct {
	RoundID        string              `json:"round_id,omitempty" format:"uuid" doc:"The approved issue for cash it converts at, when one was named." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a02"`
	RoundPrice     string              `json:"round_price" doc:"The round's price per share in the base currency." example:"3.00"`
	Currency       string              `json:"currency" doc:"The base currency." example:"SGD"`
	SharesBefore   string              `json:"shares_before" doc:"C₀: the shares issued and held at the end of the day before the round (today, for a price typed in)." example:"1000000"`
	PostMoneyPart  string              `json:"post_money_part,omitempty" doc:"f: the part of the company the post-money SAFEs convert into, Σ amount ÷ cap, as a percentage shown under RND-DISPLAY to 2 decimals; absent when none converts." example:"5.00"`
	Capitalisation string              `json:"capitalisation,omitempty" doc:"The company's capitalisation once the post-money SAFEs convert, C₀ ÷ (1 − f), shown under RND-DISPLAY to 2 decimals; absent when none converts." example:"1052631.58"`
	Conversions    []ConversionWorking `json:"conversions" nullable:"false" doc:"Each recorded SAFE or note that converts at the round, in the order recorded."`
}

// ConversionWorking is one instrument's conversion as a preview shows it.
type ConversionWorking struct {
	InstrumentID   string `json:"instrument_id" format:"uuid" doc:"The instrument." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	HolderName     string `json:"holder_name" doc:"Its holder." example:"Hatch Capital Pte. Ltd."`
	Form           string `json:"form" doc:"Its form." example:"post_money_safe"`
	Amount         Money  `json:"amount" doc:"What it converts from: the investor's money."`
	OwnershipAtCap string `json:"ownership_at_cap,omitempty" doc:"A post-money SAFE's: amount ÷ cap, the part of the company its cap gives it, as a percentage shown under RND-DISPLAY." example:"5.00"`
	CapPrice       string `json:"cap_price,omitempty" doc:"A capped SAFE's cap price before its precision, cut to 10 decimals: a post-money SAFE's cap × (1 − f) ÷ C₀, a pre-money one's cap ÷ C₀." example:"2.375"`
	DiscountPrice  string `json:"discount_price,omitempty" doc:"With a discount: the round's price × (100 − discount) ÷ 100, before its precision." example:"2.4"`
	PriceDecimals  int    `json:"price_decimals" doc:"The decimals its terms give the conversion price." example:"4"`
	Price          string `json:"price" doc:"The conversion price: the lower of the cap price and the discounted round price (the round's price itself with neither), cut down to its decimals." example:"2.375"`
	Shares         string `json:"shares" doc:"RND-SHARE: the most whole shares with round(shares × price) within the amount." example:"52631"`
	Converted      Money  `json:"converted" doc:"round(shares × price): Dr 2700 · Cr the class once effective."`
	Left           Money  `json:"left" doc:"What stays on 2700 until it is settled under the terms."`
	Covered        bool   `json:"covered" doc:"An enabled template covers every term, so its draft is ready for approval." example:"true"`
	Review         string `json:"review,omitempty" doc:"Otherwise what the accountant completes in its draft (⚠)." example:"the accountant completes it: it follows no template, a discount"`
}

// InstrumentPage is a page of instruments, the latest signed first.
type InstrumentPage struct {
	Items      []InstrumentView `json:"items" nullable:"false"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// InstrumentTemplateView is a form the engine may convert by itself, and whether the organisation enabled it.
type InstrumentTemplateView struct {
	Key              string `json:"key" doc:"The template." example:"yc-sg-post-money-cap@1"`
	Name             string `json:"name" doc:"Its name." example:"SAFE: Valuation Cap, No Discount (Singapore)"`
	Form             string `json:"form" doc:"The form it converts." example:"post_money_safe"`
	Covers           string `json:"covers" doc:"What it converts by itself; any other term converts by a draft for the accountant." example:"A post-money valuation cap, no discount, no most-favoured-nation clause and no interest, converting into shares at the next priced round."`
	Source           string `json:"source" doc:"Where its terms come from, and what is still to verify." example:"Y Combinator's Singapore SAFE [verify the conversion terms with a Singapore lawyer]"`
	Enabled          bool   `json:"enabled" doc:"The organisation recorded its enablement, with its lawyer's confirmation." example:"false"`
	EnablementID     string `json:"enablement_id,omitempty" format:"uuid" doc:"Its enablement, drafted or recorded." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b21"`
	EnablementStatus string `json:"enablement_status,omitempty" enum:"draft,recorded" doc:"draft: waiting for the lawyer's confirmation and approval; recorded: enabled." example:"draft"`
	Evidence         int    `json:"evidence,omitempty" doc:"The files attached to its enablement: asking for approval needs one, the lawyer's confirmation." example:"1"`
	ApprovalID       string `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b31"`
	Version          int    `json:"version,omitempty" doc:"Its enablement's version; with its status it makes the ETag." example:"1"`
}

// InstrumentTemplatePage is a page of the templates Fyniq knows.
type InstrumentTemplatePage struct {
	Items      []InstrumentTemplateView `json:"items" nullable:"false"`
	NextCursor string                   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6InljIn0"`
}

// EnablementDraft is what drafting a template's enablement takes.
type EnablementDraft struct {
	Note string `json:"note,omitempty" maxLength:"1000" doc:"A note, such as the lawyer and the date of their advice." example:"Advice of 3 Nov 2026"`
}

type (
	InstrumentPath struct {
		InstrumentID string `path:"instrument_id" format:"uuid" doc:"The instrument." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	}
	conversionPreviewInput struct {
		RoundID string `query:"round_id" format:"uuid" doc:"An approved issue for cash, the priced round, to convert at its price over the shares before it. Give this or price." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a02"`
		Price   string `query:"price" pattern:"^([0-9]{1,10}(\\.[0-9]{1,6})?)?$" doc:"A round's price per share in the base currency, at most 6 decimals, over the shares issued today: a round not recorded yet. Give this or round_id." example:"3.00"`
	}
	conversionPreviewOutput struct{ Body ConversionPreview }
	InstrumentTemplatePath  struct {
		TemplateKey string `path:"template_key" doc:"The template." example:"yc-sg-post-money-cap@1"`
	}
	EnablementPath struct {
		EnablementID string `path:"enablement_id" format:"uuid" doc:"The template's enablement." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b21"`
	}
)

type (
	instrumentListInput  struct{ PageQuery }
	instrumentListOutput struct{ Body InstrumentPage }
	instrumentDraftInput struct {
		KeyHeader
		PreferHeader
		Body InstrumentDraft
	}
	instrumentReviseInput struct {
		InstrumentPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body InstrumentDraft
	}
	instrumentAskInput struct {
		InstrumentPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	instrumentReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body InstrumentView
	}
	instrumentWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     InstrumentView
	}
	instrumentTemplateListOutput struct{ Body InstrumentTemplatePage }
	enablementDraftInput         struct {
		InstrumentTemplatePath
		KeyHeader
		PreferHeader
		Body EnablementDraft
	}
	enablementAskInput struct {
		EnablementPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	enablementWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     InstrumentTemplateView
	}
)

func instrumentDraftOf(i equity.Instrument) InstrumentDraft {
	out := InstrumentDraft{Form: i.Form, HolderID: i.HolderID.String(), Amount: i.Amount.String(), SignedOn: i.SignedOn.Format(time.DateOnly),
		ValuationCap: i.ValuationCap, Discount: i.Discount, MFN: i.MFN, InterestRate: i.InterestRate, Trigger: i.Trigger,
		ClassAccountID: i.ClassAccountID.String(), Denominator: i.Denominator, DenominatorNote: i.DenominatorNote, InterestConverts: i.InterestConverts,
		PriceDecimals: i.PriceDecimals, Classification: i.Classification, Template: i.Template, ProRata: i.ProRata, Note: i.Note}
	if i.MaturityOn != nil {
		out.MaturityOn = i.MaturityOn.Format(time.DateOnly)
	}
	if i.ClassID != nil {
		out.ClassID = i.ClassID.String()
	}
	return out
}

// instrumentIn reads a draft's terms, or what of them is wrong with its fields.
func instrumentIn(d InstrumentDraft, base money.Currency, today time.Time) (equity.Instrument, error) {
	var bad []FieldProblem
	field := func(f, msg, fix string) {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + f, Message: msg, Fix: fix})
	}
	i := equity.Instrument{Form: d.Form, ValuationCap: strings.TrimSpace(d.ValuationCap), Discount: strings.TrimSpace(d.Discount), MFN: d.MFN,
		InterestRate: strings.TrimSpace(d.InterestRate), Trigger: d.Trigger, Denominator: d.Denominator, DenominatorNote: strings.TrimSpace(d.DenominatorNote),
		InterestConverts: d.InterestConverts, PriceDecimals: d.PriceDecimals, Classification: d.Classification, Template: strings.TrimSpace(d.Template),
		ProRata: d.ProRata, Note: strings.TrimSpace(d.Note), Date: today}
	var err error
	if d.HolderID != "" {
		if i.HolderID, err = uuid.Parse(d.HolderID); err != nil {
			field("holder_id", "is not a contact's ID", "Send the holder's contact ID")
		}
	}
	if i.Amount, err = money.Parse(d.Amount, base); err != nil {
		field("amount", "is not an amount in the base currency", "Send it as a plain decimal, such as 125000.00")
	}
	if i.SignedOn, err = time.Parse(time.DateOnly, d.SignedOn); err != nil {
		field("signed_on", "is not a date", "Send the day as YYYY-MM-DD")
	}
	if d.MaturityOn != "" {
		m, err := time.Parse(time.DateOnly, d.MaturityOn)
		if err != nil {
			field("maturity_on", "is not a date", "Send the day as YYYY-MM-DD")
		}
		i.MaturityOn = &m
	}
	if i.ClassAccountID, err = uuid.Parse(d.ClassAccountID); err != nil {
		field("class_account_id", "is not an account's ID", "Send the share-capital account's ID")
	}
	if d.ClassID != "" {
		id, err := uuid.Parse(d.ClassID)
		if err != nil {
			field("class_id", "is not a share class's ID", "Send a recorded class's ID, or leave it out")
		}
		i.ClassID = &id
	}
	if len(bad) > 0 {
		return i, invalid(bad)
	}
	return i, nil
}

// instrumentView reads what showing instruments needs once: names, codes, the approvals waiting and the enabled templates.
type instrumentView struct {
	names   map[uuid.UUID]string
	codes   map[uuid.UUID]string
	asked   map[string]string
	enabled map[string]bool
	ledger  uuid.UUID // 2700, the account of role convertible-instruments; uuid.Nil when the chart has none
	today   time.Time
	// The conversions standing, by instrument, read once a list or record asks for them.
	conversions map[uuid.UUID]storage.Document
}

func (a *API) instrumentViewOf(ctx context.Context, u storage.UnitOfWork) (instrumentView, error) {
	v := instrumentView{names: map[uuid.UUID]string{}, codes: map[uuid.UUID]string{}, enabled: map[string]bool{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.codes[x.ID] = x.Code
		if x.Role == ledger.RoleConvertibleInstruments && x.Active && !x.Group {
			v.ledger = x.ID
		}
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return v, err
	}
	if v.asked, err = a.waiting(ctx, u, approval.ObjectInstrument); err != nil {
		return v, err
	}
	all, err := equity.Enablements(ctx, u)
	if err != nil {
		return v, err
	}
	for key, docs := range all {
		v.enabled[key] = slices.ContainsFunc(docs, func(d storage.Document) bool { return d.Status == storage.DocumentPosted })
	}
	v.conversions, err = equity.StandingConversions(ctx, u)
	return v, err
}

func (a *API) instrumentOf(ctx context.Context, u storage.UnitOfWork, v instrumentView, doc storage.Document) (InstrumentView, error) {
	i, err := equity.ReadInstrument(doc)
	if err != nil {
		return InstrumentView{}, err
	}
	name, ok := v.names[i.HolderID]
	if !ok {
		c, err := u.Contacts().Get(ctx, i.HolderID)
		if err != nil {
			return InstrumentView{}, err
		}
		name = c.LegalName
		v.names[i.HolderID] = name
	}
	out := InstrumentView{InstrumentDraft: instrumentDraftOf(i), ID: doc.ID.String(), Status: "draft", Version: doc.Version, HolderName: name,
		Currency: i.Amount.Currency().Code, ClassCode: v.codes[i.ClassAccountID], TemplateEnabled: i.Template != "" && v.enabled[i.Template]}
	if doc.Status == storage.DocumentPosted {
		out.Status = "recorded"
		if v.ledger != uuid.Nil {
			held, err := balance.Instrument(ctx, u, i.Amount.Currency(), v.ledger, doc.ID, v.today)
			if err != nil {
				return InstrumentView{}, err
			}
			out.Held = &Money{Amount: held.String(), Currency: held.Currency().Code}
		}
	}
	if out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, v.asked); err != nil {
		return out, err
	}
	if c, ok := v.conversions[doc.ID]; ok {
		ref := &InstrumentConversionRef{TransactionID: c.ID.String(), Number: c.Number, Status: "draft"}
		if c.Status == storage.DocumentPosted {
			moved, err := u.Holdings().OfTransaction(ctx, c.ID)
			if err != nil {
				return out, err
			}
			ref.Status = map[bool]string{true: "effective", false: "approved"}[len(moved) > 0]
		}
		out.Conversion = ref
	}
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the signed instrument"
		}
	}
	return out, nil
}

func (a *API) shownInstrument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (InstrumentView, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentInstrument, id)
	if err != nil {
		return InstrumentView{}, err
	}
	v, err := a.instrumentViewOf(ctx, u)
	if err != nil {
		return InstrumentView{}, err
	}
	return a.instrumentOf(ctx, u, v, doc)
}

// templatesOf are the templates Fyniq knows, each with the organisation's enablement: the recorded one, or else the draft.
func (a *API) templatesOf(ctx context.Context, u storage.UnitOfWork) ([]InstrumentTemplateView, error) {
	all, err := equity.Enablements(ctx, u)
	if err != nil {
		return nil, err
	}
	asked, err := a.waiting(ctx, u, approval.ObjectInstrumentTemplate)
	if err != nil {
		return nil, err
	}
	out := []InstrumentTemplateView{}
	for _, t := range equity.Templates {
		x := InstrumentTemplateView{Key: t.Key, Name: t.Name, Form: t.Form, Covers: t.Covers, Source: t.Source}
		var chosen *storage.Document
		for i, d := range all[t.Key] {
			if d.Status == storage.DocumentPosted || (chosen == nil && d.Status == storage.DocumentDraft) {
				chosen = &all[t.Key][i]
			}
		}
		if chosen != nil {
			x.EnablementID, x.Version, x.Enabled = chosen.ID.String(), chosen.Version, chosen.Status == storage.DocumentPosted
			x.EnablementStatus = map[bool]string{true: "recorded", false: "draft"}[x.Enabled]
			if x.Evidence, err = attachedFiles(ctx, u, chosen.ID); err != nil {
				return nil, err
			}
			x.ApprovalID = asked[chosen.ID.String()]
		}
		out = append(out, x)
	}
	return out, nil
}

func (a *API) templateOfEnablement(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (InstrumentTemplateView, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentInstrumentTemplate, id)
	if err != nil {
		return InstrumentTemplateView{}, err
	}
	e, err := equity.ReadEnablement(doc)
	if err != nil {
		return InstrumentTemplateView{}, err
	}
	all, err := a.templatesOf(ctx, u)
	if err != nil {
		return InstrumentTemplateView{}, err
	}
	for _, x := range all {
		if x.Key == e.Template {
			return x, nil
		}
	}
	return InstrumentTemplateView{}, problem(CodeNotFound, "There is no such template.", "List the templates to find it.")
}

// instrumentDependencies are an instrument's envelope values: its status and its holder (FR-APR-05).
func instrumentDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	i, err := equity.ReadInstrument(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "holder": i.HolderID.String()}, nil
}

// enablementDependencies are a template enablement's envelope values: its status and its template.
func enablementDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	e, err := equity.ReadEnablement(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "template": e.Template}, nil
}

// instrumentExecutable refuses the holder themselves: the member linked to the holder never records their own
// instrument (FM-1).
func instrumentExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	i, err := equity.ReadInstrument(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, i.HolderID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeNotApprover, "This is your own instrument, so another person approves it (FM-1).", "Ask another person who holds the approval right")
	}
	return nil
}

func instrumentAnswer(w written[InstrumentView]) *instrumentWriteOutput {
	if w.Previewed {
		return &instrumentWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	status := "draft"
	if w.Body.Status != "draft" {
		status = "posted"
	}
	return &instrumentWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/instruments/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func registerInstruments(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListInstruments, huma.Operation{
		OperationID: "listInstruments", Method: http.MethodGet, Path: "/v1/instruments", Tags: []string{"Equity"},
		Summary: "List the SAFEs and convertible notes",
		Description: "Every SAFE and convertible note, drafts and recorded, the latest signed first (FR-EQ-18): its form, holder, amount, " +
			"cap, discount, interest and maturity, the class it converts into, and whether its template is enabled. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *instrumentListInput) (*instrumentListOutput, error) {
		p, _ := authn.From(ctx)
		out := instrumentListOutput{Body: InstrumentPage{Items: []InstrumentView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := equity.Instruments(ctx, u)
			if err != nil {
				return err
			}
			v, err := a.instrumentViewOf(ctx, u)
			if err != nil {
				return err
			}
			all := make([]InstrumentView, 0, len(docs))
			for _, d := range docs {
				x, err := a.instrumentOf(ctx, u, v, d)
				if err != nil {
					return err
				}
				all = append(all, x)
			}
			slices.SortStableFunc(all, func(x, y InstrumentView) int {
				if c := strings.Compare(y.SignedOn, x.SignedOn); c != 0 {
					return c
				}
				return strings.Compare(x.ID, y.ID)
			})
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x InstrumentView) string { return x.SignedOn + "|" + x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []InstrumentView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateInstrument, huma.Operation{
		OperationID: "createInstrument", Method: http.MethodPost, Path: "/v1/instruments", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a SAFE or convertible note",
		Description: "Drafts a SAFE or convertible note (FR-EQ-18): its form, holder (a contact marked investor or shareholder), amount " +
			"in the base currency, the day it was signed, its cap, discount, most-favoured-nation clause, interest and maturity, what " +
			"converts it, the class it converts into, how its terms count the shares, the conversion price's precision and the " +
			"accountant's classification [verify per instrument]. Attach the signed instrument with addInstrumentEvidence, then ask " +
			"another person to approve it with requestInstrumentApproval. Side effects: the draft, audit events. Every problem comes " +
			"back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), InstrumentCodes...), func(ctx context.Context, in *instrumentDraftInput) (*instrumentWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InstrumentView, error) {
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return InstrumentView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return InstrumentView{}, err
			}
			i, err := instrumentIn(in.Body, s.Base, today)
			if err != nil {
				return InstrumentView{}, err
			}
			doc, err := equity.DraftInstrumentIn(ctx, u, i, s)
			if err != nil {
				return InstrumentView{}, equityProblems(err)
			}
			return a.shownInstrument(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return instrumentAnswer(w), nil
	})

	register(a, authz.GetInstrument, huma.Operation{
		OperationID: "getInstrument", Method: http.MethodGet, Path: "/v1/instruments/{instrument_id}", Tags: []string{"Equity"},
		Summary:     "Get a SAFE or convertible note",
		Description: "Returns an instrument with its ETag: its terms, its status, how many files are attached and the approval waiting (FR-EQ-18). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *InstrumentPath) (*instrumentReadOutput, error) {
		id, err := parseID("instrument_id", in.InstrumentID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out instrumentReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentInstrument, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownInstrument(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceInstrument, huma.Operation{
		OperationID: "replaceInstrument", Method: http.MethodPut, Path: "/v1/instruments/{instrument_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted SAFE or convertible note",
		Description: "Saves a new version of an instrument not recorded yet (FR-EQ-18); its holder stays. A recorded one changes no more " +
			"(EQUITY-05). Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), InstrumentCodes...), func(ctx context.Context, in *instrumentReviseInput) (*instrumentWriteOutput, error) {
		id, err := parseID("instrument_id", in.InstrumentID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InstrumentView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentInstrument, id)
			if err != nil {
				return InstrumentView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return InstrumentView{}, err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return InstrumentView{}, err
			}
			body := in.Body
			body.HolderID = ""
			i, err := instrumentIn(body, s.Base, time.Time{})
			if err != nil {
				return InstrumentView{}, err
			}
			if _, err := equity.ReviseInstrumentIn(ctx, u, id, i, s); err != nil {
				return InstrumentView{}, equityProblems(err)
			}
			return a.shownInstrument(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return instrumentAnswer(w), nil
	})

	register(a, authz.RequestInstrumentApproval, huma.Operation{
		OperationID: "requestInstrumentApproval", Method: http.MethodPost, Path: "/v1/instruments/{instrument_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a SAFE or convertible note",
		Description: "Asks another person to approve a drafted instrument at the version read (FR-EQ-18, FM-1): an instrument is " +
			"recorded only this way. It needs the signed instrument attached (EQUITY-06). The answer is 202 with an approval request, " +
			"which a person who holds A on \"Cap table, holders, investors, equity transactions\" approves, stepped up: never the one " +
			"who asked, nor the holder. The request holds the instrument's version, status and holder, so a change voids it. Needs a " +
			"person signed in who passed step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), InstrumentCodes...),
		func(ctx context.Context, in *instrumentAskInput) (*equityAskOutput, error) {
			id, err := parseID("instrument_id", in.InstrumentID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentInstrument, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the instrument has no evidence attached",
						Fix: "Attach the signed instrument, then ask again"}
				}
				i, err := equity.ReadInstrument(doc)
				if err != nil {
					return Approval{}, err
				}
				c, err := u.Contacts().Get(ctx, i.HolderID)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "form", After: i.Form},
					{Field: "amount", After: i.Amount.String() + " " + i.Amount.Currency().Code}}
				if i.ValuationCap != "" {
					diff = append(diff, ApprovalDiff{Field: "valuation_cap", After: i.ValuationCap})
				}
				if i.Discount != "" {
					diff = append(diff, ApprovalDiff{Field: "discount", After: i.Discount + "%"})
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectInstrument, ObjectKey: doc.ID.String(),
					Operation: approval.OpInstrumentRecord, Summary: fmt.Sprintf("Record %s's %s", c.LegalName, strings.ReplaceAll(i.Form, "_", " ")),
					Execute: map[string]any{"instrument_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "holder_id": i.HolderID.String()},
					Diff:    diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.PreviewInstrumentConversions, huma.Operation{
		OperationID: "previewInstrumentConversions", Method: http.MethodGet, Path: "/v1/instruments/conversion-preview", Tags: []string{"Equity"},
		Summary: "Preview the SAFEs' and notes' conversions at a round",
		Description: "Works out what every recorded SAFE and note not converted yet would convert into at a priced round (FR-EQ-18, design " +
			"D6), with the figures each conversion price comes from: at an approved issue for cash (round_id), over the shares before " +
			"it, or at a price typed in, over the shares issued today. A post-money SAFE's cap price is its cap × (1 − f) ÷ C₀, f " +
			"being Σ amount ÷ cap over the post-money SAFEs; a pre-money one's its cap ÷ C₀; the conversion price is the lower of the " +
			"cap price and the discounted round price, cut down to the instrument's decimals; the shares are RND-SHARE's. Read-only; " +
			"nothing posts and nothing is saved: draftInstrumentConversions drafts them at an approved round.",
	}, append([]string{CodeNotFound}, append(EquityTransactionCodes, InstrumentCodes...)...), func(ctx context.Context, in *conversionPreviewInput) (*conversionPreviewOutput, error) {
		if (in.RoundID == "") == (in.Price == "") {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.round_id", Message: "names neither a round nor a price, or both",
				Fix: "Send round_id, an approved issue for cash, or price, a round's price per share"}})
		}
		p, _ := authn.From(ctx)
		var out conversionPreviewOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			settings, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			var preview equity.Preview
			if in.RoundID != "" {
				id, err := parseID("round_id", in.RoundID)
				if err != nil {
					return err
				}
				if _, err := equityRecordOfType(ctx, u, storage.DocumentEquityTransaction, id); err != nil {
					return err
				}
				preview, err = equity.RoundPreviewIn(ctx, u, id, settings, today)
				if err != nil {
					return err
				}
			} else {
				price, err := money.ParseUnitPrice(in.Price, settings.Base, "price")
				if err != nil || price.Sign() <= 0 {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.price", Message: fmt.Sprintf("%q is not a price above zero in %s", in.Price, settings.Base.Code),
						Fix: "Send the round's price per share, such as 3.00"}})
				}
				if preview, err = equity.PricePreviewIn(ctx, u, price, today); err != nil {
					return err
				}
			}
			out.Body, err = a.previewOf(ctx, u, preview)
			return err
		})
		return &out, err
	})

	register(a, authz.ListInstrumentTemplates, huma.Operation{
		OperationID: "listInstrumentTemplates", Method: http.MethodGet, Path: "/v1/instrument-templates", Tags: []string{"Equity"},
		Summary: "List the instrument templates",
		Description: "The forms the engine may convert by itself (FR-EQ-18, question 33). Only YC's Singapore post-money SAFE with a " +
			"valuation cap and no discount is known. Each one shows what it covers, where its terms come from, and the " +
			"organisation's enablement. A template converts by itself only once enabled with its lawyer's confirmation. Every other " +
			"instrument, and any term a template does not cover, converts by a draft for the accountant. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *instrumentListInput) (*instrumentTemplateListOutput, error) {
		p, _ := authn.From(ctx)
		var out instrumentTemplateListOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			items, err := a.templatesOf(ctx, u)
			if err != nil {
				return err
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(items, func(x InstrumentTemplateView) string { return x.Key }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []InstrumentTemplateView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.DraftInstrumentTemplateEnablement, huma.Operation{
		OperationID: "draftInstrumentTemplateEnablement", Method: http.MethodPost, Path: "/v1/instrument-templates/{template_key}/enablement",
		Tags: []string{"Equity"}, DefaultStatus: http.StatusCreated, Summary: "Draft a template's enablement",
		Description: "Drafts the enablement of a template, or returns the draft already waiting (FR-EQ-18, question 33). Attach the " +
			"lawyer's confirmation of its terms with addInstrumentTemplateEvidence, then ask another person to approve it with " +
			"requestInstrumentTemplateApproval. A template enabled already is EQUITY-15. Side effects: the draft, audit events. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), InstrumentCodes...), func(ctx context.Context, in *enablementDraftInput) (*enablementWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InstrumentTemplateView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return InstrumentTemplateView{}, err
			}
			doc, err := equity.DraftEnablementIn(ctx, u, in.TemplateKey, in.Body.Note, today)
			if err != nil {
				return InstrumentTemplateView{}, equityProblems(err)
			}
			return a.templateOfEnablement(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		out := &enablementWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
		if !w.Previewed {
			out.ETag = etag(w.Body.Version, "draft")
		}
		return out, nil
	})

	register(a, authz.RequestInstrumentTemplateApproval, huma.Operation{
		OperationID: "requestInstrumentTemplateApproval", Method: http.MethodPost, Path: "/v1/instrument-template-enablements/{enablement_id}/approval",
		Tags: []string{"Equity"}, DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to enable a template",
		Description: "Asks another person to approve a template's enablement at the version read (FR-EQ-18, question 33, FM-1). It needs " +
			"the lawyer's confirmation attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A " +
			"on \"Cap table, holders, investors, equity transactions\" approves, stepped up, never the one who asked. On the approval " +
			"the template is enabled. Needs a person signed in who passed step-up, and If-Match with its ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), InstrumentCodes...),
		func(ctx context.Context, in *enablementAskInput) (*equityAskOutput, error) {
			id, err := parseID("enablement_id", in.EnablementID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentInstrumentTemplate, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the enablement has no evidence attached",
						Fix: "Attach the lawyer's confirmation of the template's terms, then ask again"}
				}
				e, err := equity.ReadEnablement(doc)
				if err != nil {
					return Approval{}, err
				}
				name := e.Template
				if t := equity.TemplateOf(e.Template); t != nil {
					name = t.Name
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectInstrumentTemplate, ObjectKey: doc.ID.String(),
					Operation: approval.OpInstrumentTemplateEnable, Summary: "Enable the template " + name,
					Execute: map[string]any{"enablement_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "template": e.Template},
					Diff:    []ApprovalDiff{{Field: "status", Before: "draft", After: "enabled"}, {Field: "template", After: e.Template}}})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}

// previewOf is a preview as the API shows it: each conversion's working, the shared figures under RND-DISPLAY.
func (a *API) previewOf(ctx context.Context, u storage.UnitOfWork, p equity.Preview) (ConversionPreview, error) {
	out := ConversionPreview{RoundPrice: p.Price.String(), Currency: p.Price.Currency().Code, SharesBefore: p.Before.Text('f'),
		Conversions: []ConversionWorking{}}
	if p.Round != uuid.Nil {
		out.RoundID = p.Round.String()
	}
	posts := false
	for _, s := range p.Safes {
		posts = posts || s.Instrument.Form == equity.FormPostMoneySAFE
	}
	if posts && p.Before.Sign() > 0 {
		var pct, rest, capitalisation apd.Decimal
		c := apd.BaseContext.WithPrecision(60)
		if _, err := c.Mul(&pct, &p.Part, apd.New(100, 0)); err != nil {
			return out, err
		}
		out.PostMoneyPart = rounding.Display(&pct, 2)
		if _, err := c.Sub(&rest, apd.New(1, 0), &p.Part); err != nil {
			return out, err
		}
		if rest.Sign() > 0 {
			if _, err := c.Quo(&capitalisation, &p.Before, &rest); err != nil {
				return out, err
			}
			out.Capitalisation = rounding.Display(&capitalisation, 2)
		}
	}
	names := map[uuid.UUID]string{}
	for k, conv := range p.Conversions {
		i := p.Safes[k].Instrument
		name, ok := names[i.HolderID]
		if !ok {
			c, err := u.Contacts().Get(ctx, i.HolderID)
			if err != nil {
				return out, err
			}
			name, names[i.HolderID] = c.LegalName, c.LegalName
		}
		w := ConversionWorking{InstrumentID: conv.ID.String(), HolderName: name, Form: i.Form, Amount: moneyOut(i.Amount), CapPrice: conv.CapPrice,
			DiscountPrice: conv.DiscountPrice, PriceDecimals: i.PriceDecimals, Price: conv.Price.String(), Shares: conv.Shares.Text('f'),
			Converted: moneyOut(conv.Converted), Left: moneyOut(conv.Left), Covered: conv.Covered}
		if i.Form == equity.FormPostMoneySAFE {
			if cap, _, err := apd.NewFromString(i.ValuationCap); err == nil && cap.Sign() > 0 {
				if w.OwnershipAtCap, err = rounding.Ownership(i.Amount.Decimal(), cap); err != nil {
					return out, err
				}
			}
		}
		if !conv.Covered {
			w.Review = equity.UncoveredText(conv)
		}
		out.Conversions = append(out.Conversions, w)
	}
	return out, nil
}
