package authz

import (
	"fmt"
	"slices"
	"strings"
)

// Right is a letter of Appendix D's legend.
type Right byte

// The rights. View is satisfied by any right on the capability (one cannot prepare, approve, post
// or release what one cannot see); Masked views with the policy's fields masked.
const (
	Create  Right = 'C' // create or prepare
	Approve Right = 'A'
	Post    Right = 'P' // post or execute
	Release Right = 'R' // release money
	View    Right = 'V'
	Masked  Right = 'M' // view masked
)

// Capability is a row of Appendix D, named exactly as the PRD names it.
type Capability string

// The capabilities the P0 endpoints use.
const (
	ChartOfAccounts     Capability = "Chart of accounts, dimensions"
	Journals            Capability = "Journals"
	OrganisationProfile Capability = "Organisation profile, financial year, defaults (rev 0.29)"
	ContactsAndItems    Capability = "Contacts and items (rev 0.33)"
	// Membership is not an Appendix D row: the caller's organisation (its name and base currency),
	// which every screen shows, is readable by every member of it. AI acts for a member; the Super
	// admin and the HR officer (Nemura only) are not members through those roles.
	Membership Capability = "(membership)"
	// Setup is first-run setup (FR-ADM-06): not an Appendix D row either. Its status is public; completing
	// it needs the one-time setup code and a person signed in, never the AI.
	Setup Capability = "(installation setup)"
	// SuperAdmins are the installation's Super admins and their one-time codes (FR-ADM-06/07/09): not
	// an Appendix D row. Who may act is the database's record of Super admins, step-up and the codes;
	// never the AI.
	SuperAdmins Capability = "(Super admins)"
	// Session is the caller's own sign-in (SEC-05): not an Appendix D row either. The providers to sign
	// in with are public; a session reads only itself, and the AI has none.
	Session Capability = "(your session)"
)

// Cell is one cell of Appendix D: the rights it grants and its qualifier, such as "limit".
type Cell struct {
	Rights    string // e.g. "CP" for "C/P (limit)"; "" for "—", "Proposal" and other words
	Qualifier string // the words after the letters, e.g. "limit"; or the whole cell when it has no letters
}

// Grants are the Appendix D cells of the capabilities the API uses, by column, copied from the PRD.
// The matrix test parses Appendix D and fails when a cell here differs from it.
var Grants = map[Capability]map[string]Cell{
	ChartOfAccounts: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"V", ""},
		"AR": {"V", ""}, "AP": {"V", ""}, "Payroll": {"V", ""}, "Releaser": {"V", ""}, "Approver": {"V", ""},
		"Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {}, "AI": {"V", ""},
	},
	OrganisationProfile: {
		"Owner": {"A", ""}, "Admin": {"C", ""}, "Controller": {"C", ""}, "Accountant": {"V", ""}, "Bookkeeper": {"V", ""},
		"AR": {"V", ""}, "AP": {"V", ""}, "Payroll": {"V", ""}, "Releaser": {"V", ""}, "Approver": {"V", ""},
		"Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", ""}, "DPO": {"V", ""}, "AI": {"V", ""},
	},
	ContactsAndItems: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"C", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"C", ""},
		"AR": {"C", ""}, "AP": {"C", ""}, "Payroll": {}, "Releaser": {"V", ""}, "Approver": {"V", ""},
		"Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {"V", ""}, "AI": {"V", ""},
	},
	Journals: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", "limit"}, "Bookkeeper": {},
		"AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""},
		"Tax agent": {"C", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal"},
	},
}

// Policy is what one operation needs.
type Policy struct {
	Operation  string // the OpenAPI operationId
	Capability Capability
	Need       Right
	// MaskedFields are the response's JSON fields masked for a caller who holds only M.
	MaskedFields []string
	// Note is what the policy does not enforce yet and where it will, stated in the contract.
	Note string
	// Public operations need no credential at all; NoOrganisation ones need a principal but no
	// organisation. Only the installation's operations (setup, Super admins) are either (a test keeps
	// it so).
	Public, NoOrganisation bool
}

// The policies of the P0 endpoints. A cell's qualifier is carried, not enforced: the amount limit
// of "C/P (limit)" and the segregation of duties and Founder-mode rules (SOD-3, FM-1) come from the
// approval engine (FR-APR, P1). AI cells without letters ("Proposal") grant nothing through the API:
// the AI's only write path is a proposal (golden rule 5), which arrives in P1.
var (
	GetOrganisation = Policy{Operation: "getOrganisation", Capability: Membership, Need: View}
	ListAccounts    = Policy{Operation: "listAccounts", Capability: ChartOfAccounts, Need: View}
	GetAccount      = Policy{Operation: "getAccount", Capability: ChartOfAccounts, Need: View}
	CreateAccount   = Policy{Operation: "createAccount", Capability: ChartOfAccounts, Need: Create,
		Note: "The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1)."}
	UpdateAccount = Policy{Operation: "updateAccount", Capability: ChartOfAccounts, Need: Create,
		Note: "The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1)."}
	ListContacts      = Policy{Operation: "listContacts", Capability: ContactsAndItems, Need: View}
	GetContact        = Policy{Operation: "getContact", Capability: ContactsAndItems, Need: View}
	CreateContact     = Policy{Operation: "createContact", Capability: ContactsAndItems, Need: Create}
	UpdateContact     = Policy{Operation: "updateContact", Capability: ContactsAndItems, Need: Create}
	ListItems         = Policy{Operation: "listItems", Capability: ContactsAndItems, Need: View}
	GetItem           = Policy{Operation: "getItem", Capability: ContactsAndItems, Need: View}
	CreateItem        = Policy{Operation: "createItem", Capability: ContactsAndItems, Need: Create}
	UpdateItem        = Policy{Operation: "updateItem", Capability: ContactsAndItems, Need: Create}
	LoadStandardChart = Policy{Operation: "loadStandardChart", Capability: ChartOfAccounts, Need: Create,
		Note: "The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1)."}
	CreateJournal             = Policy{Operation: "createJournal", Capability: Journals, Need: Create}
	BulkCreateJournals        = Policy{Operation: "bulkCreateJournals", Capability: Journals, Need: Create}
	GetOrganisationProfile    = Policy{Operation: "getOrganisationProfile", Capability: OrganisationProfile, Need: View}
	UpdateOrganisationProfile = Policy{Operation: "updateOrganisationProfile", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves changes to the profile; the approval itself arrives with the approval engine (P1)."}
	ListGSTRegistrations = Policy{Operation: "listGSTRegistrations", Capability: OrganisationProfile, Need: View}
	ListSequences        = Policy{Operation: "listSequences", Capability: OrganisationProfile, Need: View}
	GetSequence          = Policy{Operation: "getSequence", Capability: OrganisationProfile, Need: View}
	UpdateSequence       = Policy{Operation: "updateSequence", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves changes to the defaults; the approval itself arrives with the approval engine (P1)."}
	GetGSTRegistration    = Policy{Operation: "getGSTRegistration", Capability: OrganisationProfile, Need: View}
	GetGSTStatus          = Policy{Operation: "getGSTStatus", Capability: OrganisationProfile, Need: View}
	CreateGSTRegistration = Policy{Operation: "createGSTRegistration", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves GST registration changes; the approval itself arrives with the approval engine (P1)."}
	UpdateGSTRegistration = Policy{Operation: "updateGSTRegistration", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves GST registration changes; the approval itself arrives with the approval engine (P1)."}
	GetJournal  = Policy{Operation: "getJournal", Capability: Journals, Need: View}
	PostJournal = Policy{Operation: "postJournal", Capability: Journals, Need: Post,
		Note: "The Accountant's amount limit and SOD-3 arrive with the approval engine (P1)."}
	ReverseJournal = Policy{Operation: "reverseJournal", Capability: Journals, Need: Post,
		Note: "A reversal voids a posted journal; FM-1's second person arrives with the approval engine (P1)."}
)

var (
	GetSetupStatus = Policy{Operation: "getSetupStatus", Capability: Setup, Need: View, Public: true}
	CompleteSetup  = Policy{Operation: "completeSetup", Capability: Setup, Need: Create, NoOrganisation: true,
		Note: "Needs the one-time setup code and a first-party sign-in; an OAuth client or the AI is refused."}
)

const superAdminNote = "Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. " +
	"P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1)."

var (
	CreateSuperAdminInvitation = Policy{Operation: "createSuperAdminInvitation", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: superAdminNote}
	AcceptSuperAdminInvitation = Policy{Operation: "acceptSuperAdminInvitation", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: "Needs a valid invitation and a person signed in to Fyniq; an OAuth client or the AI is refused."}
	ReplaceRecoveryCodes = Policy{Operation: "replaceSuperAdminRecoveryCodes", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: superAdminNote}
	ConfirmSuperAdminRecovery = Policy{Operation: "confirmSuperAdminRecovery", Capability: SuperAdmins, Need: Approve, NoOrganisation: true,
		Note: superAdminNote + " The other Super admin confirms; nobody confirms their own recovery."}
	RecoverSuperAdmin = Policy{Operation: "recoverSuperAdmin", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: "Needs two proofs (SEC-35): a recovery code with the other Super admin's confirmation, or the deployment's two-person recovery; " +
			"and a person signed in to Fyniq. An OAuth client or the AI is refused."}
)

var (
	ListSignInProviders = Policy{Operation: "listSignInProviders", Capability: Session, Need: View, Public: true}
	GetSession          = Policy{Operation: "getSession", Capability: Session, Need: View, NoOrganisation: true,
		Note: "Needs a person signed in to Fyniq; an OAuth client or the AI is refused."}
)

// Policies are every policy, by operation. An operation without one is refused.
var Policies = index(GetOrganisation, GetOrganisationProfile, UpdateOrganisationProfile, ListGSTRegistrations, GetGSTRegistration,
	GetGSTStatus, CreateGSTRegistration, UpdateGSTRegistration, ListSequences, GetSequence, UpdateSequence, ListAccounts, LoadStandardChart, CreateAccount, UpdateAccount, ListContacts, GetContact,
	CreateContact, UpdateContact, ListItems, GetItem, CreateItem, UpdateItem, GetAccount, CreateJournal, BulkCreateJournals, GetJournal, PostJournal, ReverseJournal,
	GetSetupStatus, CompleteSetup, CreateSuperAdminInvitation, AcceptSuperAdminInvitation, ReplaceRecoveryCodes,
	ConfirmSuperAdminRecovery, RecoverSuperAdmin, ListSignInProviders, GetSession)

func index(ps ...Policy) map[string]Policy {
	out := make(map[string]Policy, len(ps))
	for _, p := range ps {
		if _, dup := out[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		out[p.Operation] = p
	}
	return out
}

// Decision is the outcome of an authorisation check.
type Decision struct {
	Allowed bool
	Masked  []string // fields to mask in the response
	Right   Right    // the right that allowed it
	Reason  string   // why it was refused, for the security log
}

// Decide checks whether a caller holding roles may run the operation of p. When ai is true the
// caller is the AI acting for a user holding roles: it gets the lesser of the user's right and the
// AI column's.
func Decide(p Policy, roles []Role, ai bool) Decision {
	if p.Operation == "" {
		return Decision{Reason: "the operation has no policy"}
	}
	if p.Capability == Setup || p.Capability == SuperAdmins || p.Capability == Session { // the codes, the database and a person's sign-in decide
		if ai {
			return Decision{Reason: "the AI cannot set up the installation, change its Super admins or hold a session"}
		}
		return Decision{Allowed: true, Right: p.Need}
	}
	best, ok := holds(p, roles)
	if ok && ai {
		var aiRight Right
		if aiRight, ok = holds(p, []Role{AI}); ok && rank(aiRight) < rank(best) {
			best = aiRight
		}
	}
	if !ok {
		who := "these roles"
		if ai {
			who = "the AI acting for these roles"
		}
		return Decision{Reason: fmt.Sprintf("%s hold no %q right on %q", who, string(p.Need), p.Capability)}
	}
	d := Decision{Allowed: true, Right: best}
	if best == Masked {
		d.Masked = slices.Clone(p.MaskedFields)
	}
	return d
}

// rank orders the ways of satisfying View: a full view is better than a masked one.
func rank(r Right) int {
	if r == Masked {
		return 0
	}
	return 1
}

// holds returns the best right among roles that satisfies p.Need.
func holds(p Policy, roles []Role) (Right, bool) {
	var best Right
	found := false
	for _, r := range roles {
		col := Column(r)
		if col == "" {
			continue
		}
		var rights string
		if p.Capability == Membership {
			rights = "V"
		} else {
			rights = Grants[p.Capability][col].Rights
		}
		for i := 0; i < len(rights); i++ {
			got, ok := satisfies(Right(rights[i]), p.Need)
			if ok && (!found || rank(got) > rank(best)) {
				best, found = got, true
			}
		}
	}
	return best, found
}

// satisfies reports whether holding have meets need, and as which right.
func satisfies(have, need Right) (Right, bool) {
	if need == View {
		if have == Masked {
			return Masked, true
		}
		return View, strings.ContainsRune("CAPRV", rune(have))
	}
	return have, have == need
}
