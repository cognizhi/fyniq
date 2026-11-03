// Package equity is the register of share issues (FR-EQ-08; PRD-001 §8.22, §8.23; p1-founders-money design D11): every
// allotment of shares with its holder, class, number, price, consideration, resolutions, ACRA lodgement reference and
// effective date. Share capital moves only with it or the opening balances (INV-13). This package holds what an issue
// is checked against before it is recorded; a conversion of a founder's balance posts one from the register.
package equity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of the register (ENG-23).
const (
	CodeIssue = "EQUITY-01" // an issue that does not fit: its holder, class, shares, price, consideration, references or date
)

// The longest references an issue keeps.
const (
	MaxResolutionRefs = 500
	MaxLodgementRef   = 100
)

// Considerations are the considerations a share is paid up with (FR-EQ-08).
var Considerations = []string{storage.ConsiderationCash, storage.ConsiderationDebtCapitalised, storage.ConsiderationServices, storage.ConsiderationSAFE}

var wholeShares = regexp.MustCompile(`^[1-9][0-9]{0,13}$`)

// Issue is an allotment as entered: the holder (a director or shareholder contact), the class (a share-capital
// account), the whole number of shares, the price per share in the base currency, the consideration, the resolution
// references, ACRA's lodgement reference and the effective date.
type Issue struct {
	HolderID       uuid.UUID
	ClassAccountID uuid.UUID
	Shares         string
	Price          string
	Consideration  string
	ResolutionRefs string
	LodgementRef   string
	EffectiveOn    time.Time
}

// Checked is an issue that fits: its shares and price exact, and its amount, round(shares × price, the currency's minor
// units) under RND-LINE.
type Checked struct {
	Issue  Issue
	Shares apd.Decimal
	Price  money.UnitPrice
	Amount money.Money
}

// CheckBeforeBooks checks an issue made before the books start (the incorporation subscription), recorded without an
// entry since the opening balances hold its share capital (design D11), every problem at once (ENG-22): a holder who is
// not a director or shareholder, a class that is not an active share-capital account, shares that are not a whole
// number above zero, a price that is not above zero, a consideration Fyniq does not know, no resolution reference, or an
// effective date on or after the books start.
func CheckBeforeBooks(ctx context.Context, u storage.UnitOfWork, i Issue, s ledger.Settings, booksStart *time.Time) (Checked, error) {
	var probs ledger.Problems
	add := func(field, msg, fix string) { probs = append(probs, ledger.Problem{Code: CodeIssue, Field: field, Message: msg, Fix: fix}) }
	out := Checked{Issue: i}
	c, err := u.Contacts().Get(ctx, i.HolderID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add("holder_id", "the holder does not exist in this organisation", "Choose the shareholder the shares were allotted to")
	case err != nil:
		return Checked{}, err
	case !c.Director && !c.Shareholder:
		add("holder_id", c.LegalName+" is not a director or shareholder", "Mark the contact a shareholder in Contacts first")
	}
	if a, ok := s.Accounts[i.ClassAccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	if !wholeShares.MatchString(i.Shares) {
		add("shares", fmt.Sprintf("%q is not a whole number of shares above zero", i.Shares), "Enter the number of shares, such as 10000")
	} else if _, _, err := out.Shares.SetString(i.Shares); err != nil {
		return Checked{}, err
	}
	price, err := money.ParseUnitPrice(i.Price, s.Base, "price")
	if err != nil || price.Sign() <= 0 {
		add("price", fmt.Sprintf("%q is not a price per share above zero in %s", i.Price, s.Base), "Enter the price per share, such as 1.00")
	} else {
		out.Price = price
	}
	known := false
	for _, x := range Considerations {
		known = known || x == i.Consideration
	}
	if !known {
		add("consideration", fmt.Sprintf("%q is not a consideration Fyniq knows", i.Consideration), "Choose cash, debt_capitalised, services or safe_or_note")
	}
	switch n := utf8.RuneCountInString(strings.TrimSpace(i.ResolutionRefs)); {
	case n == 0:
		add("resolution_refs", "the issue names no resolution", "Name the shareholders' approval and the board resolution, such as the incorporation subscription")
	case n > MaxResolutionRefs:
		add("resolution_refs", fmt.Sprintf("the references have %d characters; the most is %d", n, MaxResolutionRefs), "Shorten them")
	}
	if utf8.RuneCountInString(strings.TrimSpace(i.LodgementRef)) > MaxLodgementRef {
		add("lodgement_ref", fmt.Sprintf("the lodgement reference is longer than %d characters", MaxLodgementRef), "Enter ACRA's reference only")
	}
	switch {
	case booksStart == nil:
		add("effective_on", "the organisation has no books start yet", "Set the books start in Settings, then record the shares issued before it")
	case !i.EffectiveOn.Before(*booksStart):
		add("effective_on", fmt.Sprintf("the issue is dated %s, on or after the books start of %s", i.EffectiveOn.Format(time.DateOnly), booksStart.Format(time.DateOnly)),
			"Only shares issued before the books start are recorded without an entry; later ones come from a conversion")
	}
	if len(probs) > 0 {
		return Checked{}, probs
	}
	q, err := money.ParseQuantity(i.Shares, "shares")
	if err != nil {
		return Checked{}, err
	}
	if out.Amount, err = rounding.Line(q, out.Price); err != nil {
		return Checked{}, err
	}
	return out, nil
}
