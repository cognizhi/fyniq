package migration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/contact"
	"github.com/cognizhi/fyniq/internal/domain/dimension"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/item"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// countries are the ISO 3166 codes of the country names the workbook's Contacts sheet uses; a name not here, such as
// "Various", leaves the contact's country empty.
var countries = map[string]string{
	"singapore": "SG", "malaysia": "MY", "indonesia": "ID", "thailand": "TH", "vietnam": "VN", "philippines": "PH", "india": "IN",
	"china": "CN", "hong kong": "HK", "taiwan": "TW", "japan": "JP", "korea": "KR", "south korea": "KR", "australia": "AU",
	"new zealand": "NZ", "usa": "US", "us": "US", "united states": "US", "united states of america": "US", "canada": "CA",
	"uk": "GB", "united kingdom": "GB", "ireland": "IE", "germany": "DE", "france": "FR", "netherlands": "NL",
	"switzerland": "CH", "sweden": "SE", "united arab emirates": "AE", "uae": "AE",
}

// countryOf is a country name's ISO 3166 code, or "".
func countryOf(name string) string {
	n := strings.TrimSpace(name)
	if len(n) == 2 && strings.ToUpper(n) == n {
		return n
	}
	return countries[strings.ToLower(n)]
}

// The set-up's kinds, as each migration record names what its row became (design D5).
const (
	KindSettings        = "settings"
	KindFinancialYear   = "financial_year"
	KindGSTRegistration = "gst_registration"
	KindChart           = "chart"
	KindAccount         = "account"
	KindDimension       = "dimension"
	KindDepartment      = "department"
	KindProject         = "project"
	KindContact         = "contact"
	KindPerson          = "person"
	KindItem            = "item"
	KindFXRate          = "fx_rate"
	KindBankRule        = "bank_rule"
)

// validUEN says whether a UEN is in ACRA's form, as the organisation's profile takes it.
func validUEN(uen string) bool {
	if strings.TrimSpace(uen) == "" {
		return false
	}
	for _, p := range organisation.Check(storage.OrganisationProfile{UEN: strings.TrimSpace(uen)}) {
		if p.Field == "uen" {
			return false
		}
	}
	return true
}

// BooksStart is the books start the path gives Fyniq: the workbook's own for its history, or the day after Carry
// Forward's "as of" date for a clean start at the cut-over (§17.1 M3).
func (w *Workbook) BooksStart(path string) time.Time {
	if path == storage.PathCleanStart {
		return w.Settings.AsOf.AddDate(0, 0, 1)
	}
	return w.Settings.BooksStart
}

// firstYearEnd is the end of the financial year that holds start: the workbook's year end, a year back at a time.
func (w *Workbook) firstYearEnd(start time.Time) time.Time {
	end := w.Settings.YearEnd
	for !end.AddDate(-1, 0, 0).Before(start) {
		end = end.AddDate(-1, 0, 0)
	}
	return end
}

// gstOf is the sg-gst code a workbook GST code maps onto; "" for none.
func (m Mapping) gstOf(code string) string {
	code = codeOf(code)
	for _, c := range m.GSTCodes {
		if c.Workbook == code {
			return c.Fyniq
		}
	}
	return code
}

// accountOf is the Fyniq account a workbook account code (or "code - name" label) maps onto.
func (m Mapping) accountOf(code string) string {
	code = codeOf(code)
	for _, a := range m.Accounts {
		if a.Workbook == code {
			return a.Fyniq
		}
	}
	return code
}

// account is the organisation's account with a code, refused when it has none.
func account(ctx context.Context, u storage.UnitOfWork, field, code string) (storage.Account, error) {
	a, err := u.Accounts().ByCode(ctx, code)
	if errors.Is(err, ports.ErrNotFound) {
		return a, Refused{{Code: CodeMapping, Field: field, Message: fmt.Sprintf("names account %s, which the organisation does not have", code),
			Fix: "Map the workbook's account onto one of the chart's, and sign the mapping again"}}
	}
	return a, err
}

// setUp is the set-up's records (design D5): the organisation's profile and its first financial year, the GST
// registration, the chart, the department and project dimensions' values, contacts, items, FX rates and bank rules.
func (w *Workbook) setUp(m Mapping, path string) []Record {
	s := w.Settings
	start := w.BooksStart(path)
	end := w.firstYearEnd(start)
	out := []Record{
		{Key: "settings", Kind: KindSettings, Origin: Origin{Sheet: "Settings"}, make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			o, err := u.Organisation(ctx)
			if err != nil {
				return Made{}, err
			}
			p := o.Profile
			p.LegalName, p.FYStartMonth = s.Company, int(s.YearStart.Month())
			if validUEN(s.UEN) { // one that is not ACRA's form is a warning of the validation report, and left out
				p.UEN = strings.TrimSpace(s.UEN)
			}
			p.BooksStart, p.FirstYearEnd = &start, &end
			if err := refused(organisation.Check(p)); err != nil {
				return Made{}, err
			}
			periods, err := u.Periods().List(ctx)
			if err != nil {
				return Made{}, err
			}
			if err := period.Fixed(o.Profile, p, len(periods)); err != nil {
				return Made{}, err
			}
			_, err = u.UpdateOrganisation(ctx, o.Version, p)
			return Made{Type: "organisation", ID: o.ID}, err
		}},
		{Key: "financial-year:" + start.Format(time.DateOnly), Kind: KindFinancialYear, Origin: Origin{Sheet: "Settings"},
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				o, err := u.Organisation(ctx)
				if err != nil {
					return Made{}, err
				}
				c, err := period.CalendarOf(o.Profile)
				if err != nil {
					return Made{}, err
				}
				existing, err := u.Periods().List(ctx)
				if err != nil {
					return Made{}, err
				}
				have := map[string]uuid.UUID{}
				for _, p := range existing {
					have[p.StartsOn.Format(time.DateOnly)] = p.ID
				}
				// Each month of the years from the books start to the workbook's year end that the organisation lacks: its
				// periods made before the years were set stay.
				var first uuid.UUID
				for n := 1; n <= max(c.Number(w.Settings.YearEnd), 1); n++ {
					for _, mo := range c.Year(n).Months {
						id, ok := have[mo.StartsOn.Format(time.DateOnly)]
						if !ok {
							p, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: mo.StartsOn, EndsOn: mo.EndsOn})
							if err != nil {
								return Made{}, err
							}
							id = p.ID
						}
						if first == uuid.Nil {
							first = id
						}
					}
				}
				return Made{Type: "period", ID: first}, nil
			}},
	}
	if s.GSTRegistered {
		// [accountant to confirm] Registered from the books start, or from the earliest invoice or bill the workbook keeps
		// when it is earlier, as the workbook charged GST on it; filing quarterly in quarters that start with the financial
		// year: the workbook keeps neither the date nor the frequency.
		from := start
		for _, ds := range [][]Document{w.Invoices, w.Bills} {
			for _, d := range ds {
				if !d.Date.IsZero() && d.Date.Before(from) {
					from = d.Date
				}
			}
		}
		r := storage.NewGSTRegistration{RegisteredFrom: from, GSTNumber: s.GSTNumber, ReturnFrequency: storage.ReturnQuarterly,
			QuarterStartMonth: (int(s.YearStart.Month())-1)%3 + 1, AccountingBasis: storage.BasisInvoice}
		out = append(out, Record{Key: "gst-registration", Kind: KindGSTRegistration, Origin: Origin{Sheet: "Settings"},
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				r.RecordedAt = e.Now
				if err := refused(gst.Check(r)); err != nil {
					return Made{}, err
				}
				regs, err := u.GSTRegistrations().List(ctx)
				if err != nil {
					return Made{}, err
				}
				for _, x := range regs { // the organisation's own registration, with the workbook's number, stands
					if x.GSTNumber == r.GSTNumber && !x.RegisteredFrom.After(from) && (x.DeregisteredFrom == nil || x.DeregisteredFrom.After(from)) {
						return Made{Type: "gst_registration", ID: x.ID}, nil
					}
				}
				x, err := u.GSTRegistrations().Create(ctx, r)
				return Made{Type: "gst_registration", ID: x.ID}, err
			}})
	}

	// The chart: the standard chart when the organisation has none, as its set-up loads it (FR-COA-01), then each account
	// the mapping creates.
	out = append(out, Record{Key: "chart", Kind: KindChart, Origin: Origin{Sheet: "Accounts"}, make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
		o, err := u.Organisation(ctx)
		if err != nil {
			return Made{}, err
		}
		n, err := u.Accounts().Count(ctx)
		if err != nil || n > 0 {
			return Made{Type: "organisation", ID: o.ID}, err
		}
		std, err := chart.Standard()
		if err != nil {
			return Made{}, err
		}
		for _, a := range std {
			if _, err := u.Accounts().Create(ctx, a.NewAccount); err != nil {
				return Made{}, err
			}
		}
		_, err = claim.LoadStarter(ctx, u, e.Now)
		return Made{Type: "organisation", ID: o.ID}, err
	}})
	for _, a := range m.Accounts {
		if a.Action != Create {
			continue
		}
		n := storage.NewAccount{Code: a.Fyniq, Name: a.Name, Type: storage.AccountType(a.Type), AccountAttributes: storage.AccountAttributes{
			ReportGroup: a.ReportGroup, CashFlowLine: a.CashFlowLine, Function: a.Function, MoneyAccount: a.MoneyAccount, DefaultGSTCode: m.gstOf(a.DefaultGST)}}
		if a.MoneyAccount {
			n.Currency = s.BaseCurrency
		}
		out = append(out, Record{Key: "account:" + a.Fyniq, Kind: KindAccount, Origin: Origin{Sheet: "Accounts", Row: a.Row},
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				if err := refused(chart.CheckAccount(n)); err != nil {
					return Made{}, err
				}
				x, err := u.Accounts().Create(ctx, n)
				return Made{Type: "account", ID: x.ID}, err
			}})
	}

	// The department and project dimensions, and the values the mapping creates (rev 0.254).
	firstUse := w.projectRows()
	for _, set := range []struct {
		kind, name, value, sheet string
		values                   []ValueMap
		origin                   func(ValueMap) Origin
	}{
		{storage.DimensionDepartment, "Department", KindDepartment, "Employees", m.Departments, func(v ValueMap) Origin {
			for _, d := range w.Departments {
				if d.Name == v.Workbook {
					return d.Origin
				}
			}
			return Origin{Sheet: "Employees"}
		}},
		{storage.DimensionProject, "Project", KindProject, "Transactions", m.Projects, func(v ValueMap) Origin {
			if o, ok := firstUse[v.Workbook]; ok {
				return o
			}
			return Origin{Sheet: "Transactions"}
		}},
	} {
		if !slices.ContainsFunc(set.values, func(v ValueMap) bool { return v.Action == Create }) {
			continue
		}
		kind, name := set.kind, set.name
		out = append(out, Record{Key: "dimension:" + kind, Kind: KindDimension, Origin: Origin{Sheet: set.sheet},
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				d, err := dimensionOf(ctx, u, kind)
				if err == nil {
					return Made{Type: "dimension", ID: d.ID}, nil
				}
				if !errors.Is(err, ports.ErrNotFound) {
					return Made{}, err
				}
				others, err := u.Dimensions().List(ctx)
				if err != nil {
					return Made{}, err
				}
				n, probs := dimension.New(kind, name, others)
				if err := refused(probs); err != nil {
					return Made{}, err
				}
				x, err := u.Dimensions().Create(ctx, n)
				return Made{Type: "dimension", ID: x.ID}, err
			}})
		for _, v := range set.values {
			if v.Action != Create {
				continue
			}
			out = append(out, Record{Key: set.value + ":" + v.Code, Kind: set.value, Origin: set.origin(v),
				make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
					d, err := dimensionOf(ctx, u, kind)
					if err != nil {
						return Made{}, err
					}
					others, err := u.Dimensions().Values(ctx, &d.ID)
					if err != nil {
						return Made{}, err
					}
					n := storage.NewDimensionValue{DimensionID: d.ID, Code: v.Code, Name: v.Workbook}
					if err := refused(dimension.NewValue(n, &d, others)); err != nil {
						return Made{}, err
					}
					x, err := u.Dimensions().CreateValue(ctx, n)
					return Made{Type: "dimension_value", ID: x.ID}, err
				}})
		}
	}

	for _, c := range w.Contacts {
		out = append(out, Record{Key: "contact:" + strings.ToLower(strings.TrimSpace(c.Name)), Kind: KindContact, Origin: c.Origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				role := strings.ToLower(c.Role)
				d := storage.ContactDetails{LegalName: strings.TrimSpace(c.Name), Customer: strings.Contains(role, "customer") || strings.Contains(role, "both"),
					Vendor: strings.Contains(role, "supplier") || strings.Contains(role, "vendor") || strings.Contains(role, "both"),
					Phone:  strings.TrimSpace(c.Phone), Address: strings.TrimSpace(c.Address), Country: countryOf(c.Country), Currency: strings.TrimSpace(c.Currency)}
				if email := strings.TrimSpace(c.Email); email != "" {
					if d.Customer {
						d.BillingEmail = email
					}
					if d.Vendor {
						d.RemittanceEmail = email
					}
				}
				if c.Terms != nil {
					days, err := c.Terms.Int64()
					if err != nil || days < 0 || days > 365 {
						return Made{}, Refused{{Code: contact.CodeTerms, Field: "payment_terms_days", Message: fmt.Sprintf("%s days is not a whole number from 0 to 365", c.Terms.Text('f')),
							Fix: "Enter the payment terms in whole days in the workbook"}}
					}
					n := int(days)
					d.PaymentTermsDays = &n
				}
				if c.Category != "" {
					a, err := account(ctx, u, "default_account_id", m.accountOf(c.Category))
					if err != nil {
						return Made{}, err
					}
					if err := refused(contact.CheckCategory(a)); err != nil {
						return Made{}, err
					}
					d.DefaultAccountID = &a.ID
				}
				if err := refused(contact.Check(d)); err != nil {
					return Made{}, err
				}
				x, err := u.Contacts().Create(ctx, d)
				return Made{Type: "contact", ID: x.ID}, err
			}})
	}

	// The people behind the director's and subscriber's accounts (rev 0.259): one contact per person, marked director or
	// shareholder, from the first row that names them.
	people := map[string]*PersonMap{}
	var order []string
	for i := range m.People {
		p := m.People[i]
		if x, ok := people[p.Contact]; ok {
			x.Director, x.Shareholder = x.Director || p.Director, x.Shareholder || p.Shareholder
			continue
		}
		people[p.Contact] = &p
		order = append(order, p.Contact)
	}
	for _, name := range order {
		p := people[name]
		out = append(out, Record{Key: "person:" + strings.ToLower(name), Kind: KindPerson, Origin: Origin{Sheet: p.Sheet, Row: p.Row},
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				d := storage.ContactDetails{LegalName: name, Director: p.Director, Shareholder: p.Shareholder, Individual: p.Director}
				if err := refused(contact.Check(d)); err != nil {
					return Made{}, err
				}
				x, err := u.Contacts().Create(ctx, d)
				return Made{Type: "contact", ID: x.ID}, err
			}})
	}

	for _, it := range w.Items {
		out = append(out, Record{Key: "item:" + it.Code, Kind: KindItem, Origin: it.Origin, make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			typ := storage.ItemProduct
			switch strings.ToLower(strings.TrimSpace(it.Type)) {
			case "service", "subscription":
				typ = storage.ItemService
			}
			d := storage.ItemDetails{Name: strings.TrimSpace(it.Name), Description: strings.TrimSpace(it.Description), Type: typ, Unit: strings.TrimSpace(it.Unit),
				GSTCode: m.gstOf(it.GST)}
			if it.Price != nil {
				d.Prices = []storage.ItemPrice{{Currency: s.BaseCurrency, UnitPrice: storage.Decimal(it.Price.Text('f'))}}
			}
			if it.RevenueAccount != "" {
				a, err := account(ctx, u, "revenue_account_id", m.accountOf(it.RevenueAccount))
				if err != nil {
					return Made{}, err
				}
				if err := refused(item.CheckAccount(a)); err != nil {
					return Made{}, err
				}
				d.RevenueAccountID = &a.ID
			}
			if err := refused(append(item.CheckCode(it.Code), item.Check(d)...)); err != nil {
				return Made{}, err
			}
			x, err := u.Items().Create(ctx, it.Code, d)
			if err == nil && !it.Active {
				x, err = u.Items().Update(ctx, x.ID, x.Version, x.ItemDetails, false)
			}
			return Made{Type: "item", ID: x.ID}, err
		}})
	}

	for _, r := range w.Rates {
		on := time.Date(r.Month.Year(), r.Month.Month(), 1, 0, 0, 0, 0, time.UTC)
		out = append(out, Record{Key: "fx-rate:" + r.Currency + ":" + on.Format("2006-01"), Kind: KindFXRate, Origin: r.Origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				base, err := money.Lookup(s.BaseCurrency)
				if err != nil {
					return Made{}, err
				}
				quote := ""
				if r.Rate != nil {
					quote = r.Rate.Text('f')
				}
				n, bad := fx.Check(fx.Entry{Kind: storage.FXRateMonth, Currency: r.Currency, On: on, Quote: quote, QuoteForm: money.QuoteBasePerUnit,
					Note: "From the workbook's Lists sheet (FR-MIG-01)"}, base)
				if len(bad) > 0 {
					out := make(Refused, len(bad))
					for i, x := range bad {
						out[i] = Refusal{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix}
					}
					return Made{}, out
				}
				x, err := fx.Enter(ctx, u, n)
				return Made{Type: "fx_rate", ID: x.ID}, err
			}})
	}

	for _, r := range m.BankRules {
		if w.transferRule(r.Order) {
			continue // kept in the archive: the validation report says so
		}
		out = append(out, Record{Key: fmt.Sprintf("bank-rule:%d", r.Order), Kind: KindBankRule, Origin: w.ruleOrigin(r.Order),
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				a, err := account(ctx, u, "account_id", r.Account)
				if err != nil {
					return Made{}, err
				}
				name := "Contains " + r.Contains
				if len([]rune(name)) > banking.MaxRuleName {
					name = string([]rune(name)[:banking.MaxRuleName])
				}
				// A rule suggests: an approver makes it auto-apply after its back-test (FR-BNK-14).
				x, err := banking.CreateRule(ctx, u, banking.RuleRequest{Name: name, Position: r.Order, When: banking.Conditions{DescriptionContains: r.Contains},
					Then: banking.Action{AccountID: a.ID, GSTCode: r.GST}, Active: true, At: e.Now})
				return Made{Type: "bank_rule", ID: x.ID}, err
			}})
	}
	return out
}

// dimensionOf is the organisation's dimension of a kind: ports.ErrNotFound when it has none.
func dimensionOf(ctx context.Context, u storage.UnitOfWork, kind string) (storage.Dimension, error) {
	dims, err := u.Dimensions().List(ctx)
	if err != nil {
		return storage.Dimension{}, err
	}
	for _, d := range dims {
		if d.Kind == kind {
			return d, nil
		}
	}
	return storage.Dimension{}, ports.ErrNotFound
}

// transferRule says whether the bank rule in a place, 1 for the first, gives one of the workbook's money accounts: money
// moving between two of the organisation's accounts, which Fyniq records as a transfer the bank import matches, never
// by a rule (FR-BNK-05), so the import keeps it in the archive.
func (w *Workbook) transferRule(order int) bool {
	if order < 1 || order > len(w.BankRules) {
		return false
	}
	code := codeOf(w.BankRules[order-1].Account)
	for _, a := range w.Accounts {
		if a.Code == code {
			return a.Money
		}
	}
	return false
}

// ruleOrigin is the Lists sheet's row of the bank rule in a place, 1 for the first.
func (w *Workbook) ruleOrigin(order int) Origin {
	if order >= 1 && order <= len(w.BankRules) {
		return w.BankRules[order-1].Origin
	}
	return Origin{Sheet: "Lists"}
}

// projectRows are each project's first row: where its dimension value comes from.
func (w *Workbook) projectRows() map[string]Origin {
	out := map[string]Origin{}
	add := func(p string, o Origin) {
		if p = strings.TrimSpace(p); p != "" {
			if _, ok := out[p]; !ok {
				out[p] = o
			}
		}
	}
	for _, x := range w.Transactions {
		add(x.Project, x.Origin)
	}
	for _, ds := range [][]Document{w.Invoices, w.Bills, w.CarryForward.Invoices, w.CarryForward.Bills} {
		for _, d := range ds {
			add(d.Project, d.Origin)
		}
	}
	for _, q := range w.QuotesOrders {
		add(q.Project, q.Origin)
	}
	for _, l := range w.Journal {
		add(l.Project, l.Origin)
	}
	for _, r := range w.Recurring {
		add(r.Project, r.Origin)
	}
	return out
}
