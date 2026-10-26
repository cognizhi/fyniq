package claim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// WarnOverLimit is a line above its category's limit (⚠, FR-AP-15): it posts, and finance approves it too (FR-AP-17).
const WarnOverLimit = "CLAIM-OVER-LIMIT"

// Warning is one ⚠ of a claim: it posts, and its approvers see why it is flagged.
type Warning struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// Prepared is a claim version as the rule reads it, with what it shows beside its entry: each line's category, how each
// line's input tax was decided (FR-AP-20), and its warnings; OverLimit says a line is above its category's limit, so
// finance approves it too (FR-AP-17; the owner's decision of 26 Oct 2026).
type Prepared struct {
	Draft      Draft
	Claim      ledger.Claim
	Categories []storage.ClaimCategory // one a line, in order; the zero category where it could not be read
	InputTax   []bill.Decision         // one a line, in order
	Warnings   []Warning
	OverLimit  bool
	Trips      map[int]Trip // a mileage line's trip, by its index in Lines
}

// Trip is what RND-MILEAGE made of a mileage line (FR-AP-16): the distance it is claimed for, to 0.1 km, the vehicle's
// rate per km on its date, and its amount.
type Trip struct {
	Km     money.Quantity
	Rate   money.Money
	Amount money.Money
}

// Prepare reads one version of a claim for the rule: its claimant, each line's category and the account it costs, its
// rate (the typed one, else the cascade's for the receipt's date), and its input tax, decided as a bill line's is from
// the receipt's facts and GST code and the organisation's GST registration on the receipt's date (FR-AP-20). Problems
// the rule would refuse come back as ledger.Problems with the engine's own, from PostExpenseClaim.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency) (Prepared, ledger.Problems, error) {
	d, err := Read(doc, base)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}

	// The claimant: an active member of the organisation.
	if d.ClaimantID == uuid.Nil {
		add(CodeClaimant, "claimant_id", "the claim has no claimant", "Claim as yourself, from My claims")
	} else {
		m, err := u.Members().Membership(ctx, d.ClaimantID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeClaimant, "claimant_id", "the claimant is not a member of this organisation", "Claim as a member of the organisation")
		case err != nil:
			return Prepared{}, nil, err
		case m.Status != "active":
			add(CodeClaimant, "claimant_id", "the claimant is no longer a member of this organisation",
				"Their claims are kept; a removed member's new claim is not posted")
		}
	}

	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return Prepared{}, nil, err
	}
	c := ledger.Claim{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: memo(d)}
	out.Trips = map[int]Trip{}
	rates := map[string][]storage.SettingChange{} // the changes of each vehicle's rate, read once
	for i, l := range d.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		if l.Kind == LineMileage {
			line, err := prepareTrip(ctx, u, d, i, l, base, rates, &out, add)
			if err != nil {
				return Prepared{}, nil, err
			}
			c.Lines = append(c.Lines, line)
			continue
		}
		cur := l.Amount.Currency()
		switch {
		case l.Date.IsZero():
			add(CodeReceipt, f("date"), "the receipt has no date", "Enter the date on the receipt")
		case !d.Date.IsZero() && l.Date.After(d.Date):
			add(CodeReceipt, f("date"), fmt.Sprintf("the receipt is dated %s, after the claim's date %s", day(l.Date), day(d.Date)),
				"Check the receipt's date, or date the claim on or after it")
		}
		if strings.TrimSpace(l.Merchant) == "" {
			add(CodeReceipt, f("merchant"), "the receipt has no merchant", "Enter who you paid, as the receipt shows it")
		}

		// Its category, and the account it costs.
		var cat storage.ClaimCategory
		if l.CategoryID == uuid.Nil {
			add(CodeCategory, f("category_id"), "the line has no category", "Choose what the expense was for")
		} else {
			cat, err = u.ClaimCategories().Get(ctx, l.CategoryID)
			switch {
			case errors.Is(err, ports.ErrNotFound):
				add(CodeCategory, f("category_id"), "the category does not exist in this organisation", "Choose one of the expense policy's categories")
			case err != nil:
				return Prepared{}, nil, err
			case !cat.Active:
				add(CodeCategory, f("category_id"), fmt.Sprintf("the category %s is no longer in use", cat.Name), "Choose another category")
			case cat.Mileage:
				add(CodeCategory, f("category_id"), fmt.Sprintf("%s is for trips in your own vehicle, not receipts", cat.Name),
					"Choose the category the receipt is for")
			}
		}
		out.Categories = append(out.Categories, cat)

		// Its rate: as typed, else the cascade's for the receipt's date (ENG-40).
		rate := l.Rate
		if cur != base && rate == nil && !l.Date.IsZero() {
			found, err := fx.Lookup(ctx, u, base, cur, l.Date)
			var fe *fx.Error
			switch {
			case errors.As(err, &fe):
				add(fe.Code, f("fx_rate"), fe.Message, fe.Fix)
			case err != nil:
				return Prepared{}, nil, err
			default:
				r := found.Rate
				rate = &r
			}
		}

		// Its input tax, decided as a bill line's is (FR-AP-20), from the sg-gst version in force on its date.
		version, err := gst.Version(ctx, u, l.Date)
		if err != nil {
			return Prepared{}, nil, err
		}
		_, registered := gst.StatusOn(regs, l.Date)
		var total *money.Money
		switch {
		case cur == base:
			total = &l.Amount
		case rate != nil:
			if t, err := rounding.FX(l.Amount, *rate); err == nil {
				total = &t
			}
		}
		dec, known := bill.DecideReceipt(bill.InputTax{Document: l.Document, AddressedTo: l.AddressedTo}, l.GST, l.GSTCode, version, registered, l.Date,
			total, base)
		if !known {
			add(CodeGSTCode, f("gst_code"), fmt.Sprintf("%q is not a GST code for purchases", l.GSTCode),
				"Choose the GST code for the receipt, such as TX when it shows GST, or NR when the merchant is not GST-registered")
		}
		out.InputTax = append(out.InputTax, dec)

		// Above its category's limit, in the base currency: what the company owes for the line (FR-AP-15).
		if cat.LineLimit != nil {
			owed, ok := l.Amount, cur == base
			switch {
			case cur != base && l.Paid != nil:
				owed, ok = *l.Paid, l.Paid.Currency() == base
			case cur != base && rate != nil:
				t, err := rounding.FX(l.Amount, *rate)
				owed, ok = t, err == nil
			}
			limit, err := money.ParseStored(string(*cat.LineLimit), base)
			if over, cmpErr := owed.Cmp(limit); ok && err == nil && cmpErr == nil && over > 0 {
				out.OverLimit = true
				out.Warnings = append(out.Warnings, Warning{WarnOverLimit, f("amount"),
					fmt.Sprintf("%s %s is above the %s category's limit of %s %s a line", owed, base, cat.Name, limit, base),
					"It can still be claimed: finance approves it too; say why in the claim's notes"})
			}
		}
		c.Lines = append(c.Lines, ledger.ClaimLine{AccountID: cat.AccountID, Description: describe(l), Amount: l.Amount, GST: l.GST,
			Claimable: dec.Condition == bill.Claimable && dec.Reason != bill.ReasonNoGST, Rate: rate, StatedBase: l.StatedGST, Paid: l.Paid})
	}
	out.Claim = c
	return out, probs, nil
}

// memo names a claim in its entry: what it is for.
func memo(d Draft) string {
	if t := strings.TrimSpace(d.Title); t != "" {
		return "Expense claim: " + t
	}
	return "Expense claim"
}

// describe is a line's description in the entry: the merchant and what it was for.
func describe(l Line) string {
	out := strings.TrimSpace(l.Merchant)
	if x := strings.TrimSpace(l.Description); x != "" {
		out = strings.TrimSpace(out + ": " + x)
	}
	for len(out) > ledger.MaxDescription { // whole characters only
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return out
}

// prepareTrip reads mileage line i for the rule (FR-AP-16): its date and the mileage category, its distance, and the
// rate per km for its vehicle in force on its date (a setting, unset until the organisation sets it); its amount is
// RND-MILEAGE's, with no GST.
func prepareTrip(ctx context.Context, u storage.UnitOfWork, d Draft, i int, l Line, base money.Currency, rates map[string][]storage.SettingChange,
	out *Prepared, add func(code, field, msg, fix string)) (ledger.ClaimLine, error) {
	f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
	none := bill.Decision{Condition: bill.Claimable, Reason: bill.ReasonNoGST, Text: "a mileage line claims no GST (GST Regulations, reg. 27)"}
	out.InputTax = append(out.InputTax, none)
	switch {
	case l.Date.IsZero():
		add(CodeReceipt, f("date"), "the trip has no date", "Enter the day of the trip")
	case !d.Date.IsZero() && l.Date.After(d.Date):
		add(CodeReceipt, f("date"), fmt.Sprintf("the trip is dated %s, after the claim's date %s", day(l.Date), day(d.Date)),
			"Check the trip's date, or date the claim on or after it")
	}
	if strings.TrimSpace(l.From) == "" || strings.TrimSpace(l.To) == "" {
		add(CodeMileage, f("from"), "the trip does not say where it was from and to", "Enter both places, or pick saved places")
	}
	var cat storage.ClaimCategory
	if l.CategoryID == uuid.Nil {
		add(CodeCategory, f("category_id"), "the trip has no category", "Choose the mileage category")
	} else {
		var err error
		cat, err = u.ClaimCategories().Get(ctx, l.CategoryID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeCategory, f("category_id"), "the category does not exist in this organisation", "Choose the mileage category")
		case err != nil:
			return ledger.ClaimLine{}, err
		case !cat.Active:
			add(CodeCategory, f("category_id"), fmt.Sprintf("the category %s is no longer in use", cat.Name), "Choose the mileage category")
		case !cat.Mileage:
			add(CodeCategory, f("category_id"), fmt.Sprintf("%s is for receipts, not trips", cat.Name), "Choose the mileage category")
		}
	}
	out.Categories = append(out.Categories, cat)
	line := ledger.ClaimLine{AccountID: cat.AccountID, Description: describeTrip(l), Amount: money.Zero(base), GST: money.Zero(base), Mileage: true}
	key, ok := Vehicles[l.Vehicle]
	if !ok || l.Date.IsZero() {
		if !ok {
			add(CodeMileage, f("vehicle"), fmt.Sprintf("%q is not a vehicle Fyniq has a rate for", l.Vehicle), "Choose car or motorcycle")
		}
		return line, nil
	}
	changes, read := rates[key]
	if !read {
		var err error
		if changes, err = u.Settings().Changes(ctx, key); err != nil {
			return ledger.ClaimLine{}, err
		}
		rates[key] = changes
	}
	def, _ := settings.Lookup(key)
	value := settings.On(def, changes, l.Date).Value
	if value == "" {
		add(CodeMileageRate, f("vehicle"), fmt.Sprintf("the organisation has set no rate per km for a %s on %s", l.Vehicle, day(l.Date)),
			"Ask the owner or the accountant to set it in Settings › Organisation, Mileage rates")
		return line, nil
	}
	rate, err := money.Parse(value, base)
	if err != nil {
		return ledger.ClaimLine{}, err
	}
	km, amount, err := rounding.Mileage(l.Distance, rate)
	if err != nil {
		add(CodeMileage, f("distance"), fmt.Sprintf("the distance is %s km", l.Distance), "Enter the distance driven, at least 0.05 km")
		return line, nil
	}
	out.Trips[i] = Trip{Km: km, Rate: rate, Amount: amount}
	line.Amount = amount
	if cat.LineLimit != nil {
		limit, err := money.ParseStored(string(*cat.LineLimit), base)
		if over, cmpErr := amount.Cmp(limit); err == nil && cmpErr == nil && over > 0 {
			out.OverLimit = true
			out.Warnings = append(out.Warnings, Warning{WarnOverLimit, f("distance"),
				fmt.Sprintf("%s %s is above the %s category's limit of %s %s a line", amount, base, cat.Name, limit, base),
				"It can still be claimed: finance approves it too; say why in the claim's notes"})
		}
	}
	return line, nil
}

// describeTrip is a trip's description in the entry: what it was for, and where from and to.
func describeTrip(l Line) string {
	trip := strings.TrimSpace(l.From) + " to " + strings.TrimSpace(l.To)
	if l.Return {
		trip += " and back"
	}
	out := strings.TrimSpace(strings.TrimSpace(l.Description) + " (" + trip + ")")
	for len(out) > ledger.MaxDescription { // whole characters only
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return out
}
