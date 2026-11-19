// Package retention is Nemura's retention rules (PRD-002 NM-SEC-04, NM-SEC-05): what is due for deletion, what a legal hold keeps, and
// when a report of due deletions must have been reviewed. A deletion is never silent: it is a report for review with a review date
// (30 days by default), after which it is overdue and shows ✖ in Checks, so "reviewed" never means kept for ever; a legal hold keeps a
// record and is listed; a record is never deleted before its statutory minimum, whatever a company policy says; and each deletion
// leaves proof of what went and when, never its content.
package retention

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReviewDays is how long a report of due deletions may wait for its review.
const ReviewDays = 30

// Record is one record of a type that may be due: its ID, the person it is about, the date its retention runs from, and what to
// fingerprint for the proof of its deletion.
type Record struct {
	ID      uuid.UUID
	Subject uuid.UUID
	From    time.Time // the date the retention period runs from, such as the leaving date
	Content []byte    // fingerprinted into the proof, never stored
}

// Hold keeps records: one person's of a type, or every record of the type when Subject is nil.
type Hold struct {
	RecordType string
	Subject    *uuid.UUID
	From       time.Time
	Until      *time.Time // nil while the hold stands
}

// Policy is how long a type is kept: the statutory minimum from the pack and the company's own period, which may be longer and
// never shorter (NM-PACK-04). Months count from the record's date.
type Policy struct {
	RecordType string
	MinMonths  int // the statutory minimum: a record younger than this is never due
	KeepMonths int // the company's period; 0 means the minimum
}

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// The codes.
const CodeBelowMinimum = "NM_RETENTION_BELOW_MINIMUM"

// Check refuses a company period shorter than the statutory minimum.
func (p Policy) Check() error {
	if p.KeepMonths != 0 && p.KeepMonths < p.MinMonths {
		return &Refusal{CodeBelowMinimum, fmt.Sprintf("%s cannot be kept for %d months: the statutory minimum is %d months. Choose %d or more.", p.RecordType, p.KeepMonths, p.MinMonths, p.MinMonths)}
	}
	return nil
}

func (p Policy) months() int {
	if p.KeepMonths > p.MinMonths {
		return p.KeepMonths
	}
	return p.MinMonths
}

// Cutoff is the date a record's retention must have started on or before for it to be due: now less the period.
func (p Policy) Cutoff(now time.Time) time.Time { return now.AddDate(0, -p.months(), 0) }

// Plan is what the deletion job found: the records due, and the ones a hold keeps.
type Plan struct {
	Due  []Record
	Held []Record
}

func (h Hold) keeps(r Record, recordType string, on time.Time) bool {
	if h.RecordType != recordType || on.Before(h.From) || (h.Until != nil && !on.Before(*h.Until)) {
		return false
	}
	return h.Subject == nil || *h.Subject == r.Subject
}

// Plan lists, of the records, those whose retention has run out on the day, and separates the ones a hold keeps. A record whose
// period has not run out is neither: it is not due. An error from Check refuses the policy first.
func (p Policy) Plan(now time.Time, records []Record, holds []Hold) (Plan, error) {
	if err := p.Check(); err != nil {
		return Plan{}, err
	}
	cutoff := p.Cutoff(now)
	var out Plan
	for _, r := range records {
		if r.From.After(cutoff) {
			continue
		}
		held := false
		for _, h := range holds {
			held = held || h.keeps(r, p.RecordType, now)
		}
		if held {
			out.Held = append(out.Held, r)
		} else {
			out.Due = append(out.Due, r)
		}
	}
	return out, nil
}

// ReviewBy is the day a report made on day must have been reviewed.
func ReviewBy(day time.Time) time.Time { return day.AddDate(0, 0, ReviewDays) }

// Overdue reports whether a report not yet reviewed has passed its review date (Checks shows ✖).
func Overdue(reviewBy, now time.Time, reviewed bool) bool { return !reviewed && now.After(reviewBy) }

// Fingerprint is what the proof of a deletion keeps of a record: a SHA-256 of its type, id and content, so it can be shown that a
// given record was the one deleted without keeping it.
func Fingerprint(recordType string, id uuid.UUID, content []byte) []byte {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|", recordType, id)
	h.Write(content)
	return h.Sum(nil)
}
