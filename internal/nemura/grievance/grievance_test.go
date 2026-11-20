package grievance

import (
	"testing"

	"github.com/google/uuid"
)

func id(s string) uuid.UUID { return uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e" + s) }

// A grievance goes to an HR administrator before the Owner, never to the person who filed it or anyone it is about, never back to the one who passes it on, and waits when nobody is left (NM-SEC-06).
// req: NM-SEC-06
func TestAGrievanceIsRoutedAwayFromThePeopleItIsAbout(t *testing.T) {
	g := id("00")
	hr1, hr2, owner, other := id("11"), id("12"), id("21"), id("31")
	cands := []Candidate{{hr1, "hr_admin"}, {hr2, "hr_admin"}, {owner, "owner"}}
	if h, ok := Route(g, cands, other, nil); !ok || (h != hr1 && h != hr2) {
		t.Errorf("the default handler is an HR administrator: %v %v", h, ok)
	}
	if h, ok := Route(g, cands, other, map[uuid.UUID]bool{hr1: true, hr2: true}); !ok || h != owner {
		t.Errorf("both HR administrators complained about: %v %v", h, ok)
	}
	if h, ok := Route(g, cands, hr1, map[uuid.UUID]bool{hr2: true}); !ok || h != owner {
		t.Errorf("the complainant is an HR administrator and the other is complained about: %v %v", h, ok)
	}
	if _, ok := Route(g, cands, other, map[uuid.UUID]bool{hr1: true, hr2: true, owner: true}); ok {
		t.Error("everyone authorised is complained about, and the grievance was given to someone")
	}
	if h, ok := Route(g, cands, other, nil, hr1); !ok || h != hr2 {
		t.Errorf("a handover goes to another: %v %v", h, ok)
	}
	if _, ok := Route(g, []Candidate{{owner, "owner"}}, owner, nil); ok {
		t.Error("the Owner who filed it was made its handler")
	}
	if _, ok := Route(g, []Candidate{{other, "hr_officer"}}, id("41"), nil); ok {
		t.Error("an HR officer was made a handler: only an HR administrator or the Owner handles")
	}
	seen := map[uuid.UUID]bool{}
	for i := 0; i < 64; i++ { // the choice among equals is spread, and the same for the same grievance
		gi := uuid.New()
		a, _ := Route(gi, cands, other, nil)
		b, _ := Route(gi, cands, other, nil)
		if a != b {
			t.Fatal("the same grievance was routed two ways")
		}
		seen[a] = true
	}
	if len(seen) != 2 {
		t.Errorf("64 grievances went to %d of the 2 HR administrators", len(seen))
	}
}

// req: NM-SEC-06
func TestWordsAreCheckedInPlainWords(t *testing.T) {
	if Words("summary", "too short") == nil || Words("summary", string(make([]rune, 4001))) == nil {
		t.Error("too short or too long was accepted")
	}
	if r := Words("outcome", "The inquiry found that the conduct complained of took place."); r != nil {
		t.Errorf("a proper outcome was refused: %v", r)
	}
}
