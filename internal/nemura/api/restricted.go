package api

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// Pass is a work pass with its number masked.
type Pass struct {
	ID      string `json:"id" format:"uuid" doc:"The pass record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e70"`
	Type    string `json:"pass_type" enum:"employment_pass,s_pass,work_permit,dp_work_permit" doc:"The kind of pass." example:"employment_pass"`
	Number  string `json:"number_masked" doc:"The number, masked." example:"*******A12"`
	Issued  string `json:"issued_on" format:"date" doc:"The day it was issued." example:"2026-01-01"`
	Expires string `json:"expires_on" format:"date" doc:"The day it expires." example:"2027-12-31"`
}

// Dependant is a child for family leave.
type Dependant struct {
	ID             string `json:"id" format:"uuid" doc:"The dependant record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
	Relationship   string `json:"relationship" enum:"child,adopted_child,stepchild" doc:"How the child is related." example:"child"`
	BirthDate      string `json:"birth_date,omitempty" format:"date" doc:"The child's date of birth; hidden at the masked level." example:"2025-05-06"`
	AdoptionIntent bool   `json:"adoption_intent" doc:"The formal intent to adopt, instead of a date of birth." example:"false"`
	Citizenship    string `json:"citizenship" enum:"citizen,permanent_resident,other" doc:"The child's citizenship." example:"citizen"`
	From           string `json:"effective_from" format:"date" doc:"The day the record starts." example:"2026-01-02"`
	To             string `json:"effective_to,omitempty" format:"date" doc:"The day it ends." example:"2030-01-02"`
}

// Document is a document's record; its content is fetched separately.
type Document struct {
	ID             string   `json:"id" format:"uuid" doc:"The document." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e72"`
	Type           string   `json:"doc_type" enum:"contract,kets,letter,pass_copy,certificate,other" doc:"The kind of document." example:"contract"`
	Title          string   `json:"title" doc:"Its title." example:"Employment contract"`
	RetentionClass string   `json:"retention_class" doc:"The retention rule it falls under." example:"employment-records"`
	ContentType    string   `json:"content_type" doc:"Its media type." example:"application/pdf"`
	Size           int64    `json:"size_bytes" doc:"Its size in bytes." example:"20480"`
	Viewers        []string `json:"viewers" doc:"The roles who may see it."`
	Created        string   `json:"created_at" format:"date-time" doc:"When it was added." example:"2026-01-05T09:00:00Z"`
}

// BankChange is a proposed change to a salary bank account, its account masked.
type BankChange struct {
	ID     string `json:"id" format:"uuid" doc:"The change." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73"`
	Person string `json:"person_id" format:"uuid" doc:"The person whose account it is." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Masked string `json:"account_masked" doc:"The proposed account, masked." example:"****7890"`
	State  string `json:"state" enum:"proposed,approved,refused,confirmed" doc:"Where the change is." example:"proposed"`
}

// IR8AParticulars are what the IR8A asks of a person besides what Nemura holds elsewhere: kept sealed with the NRIC.
type IR8AParticulars struct {
	Sex         string `json:"sex" enum:"M,F" doc:"M or F, as the IR8A takes it (data item B6)." example:"F"`
	AddressType string `json:"address_type,omitempty" enum:"L,F" doc:"L for a Singapore address, F for a foreign one; leave it out when there is no address (B7)." example:"L"`
	Address1    string `json:"address_1,omitempty" maxLength:"100" doc:"The first address line: a real address, never NIL or N.A. (B8)." example:"Blk 123 Choa Chu Kang Rd #10-01"`
	Address2    string `json:"address_2,omitempty" maxLength:"100" doc:"The second address line; only after the first." example:"Singapore"`
	Address3    string `json:"address_3,omitempty" maxLength:"100" doc:"The third address line; only after the second." example:"Level 3"`
	PostalCode  string `json:"postal_code,omitempty" pattern:"^[0-9]{6}$" doc:"The six-digit post code of a Singapore address (B9)." example:"670123"`
	Country     string `json:"country,omitempty" maxLength:"3" doc:"IRAS's country code of a foreign address (B10)." example:"GB"`
	Designation string `json:"designation,omitempty" maxLength:"30" doc:"A non-resident director's or seaman's designation (B11)." example:"Director"`
}

// Restricted is the restricted people data behind the API; the API decides who may call, the service seals, reads at a level and audits.
type Restricted interface {
	SetNRIC(ctx context.Context, s Session, id uuid.UUID, value string) error
	SetIR8AParticulars(ctx context.Context, s Session, id uuid.UUID, p IR8AParticulars) error
	Secret(ctx context.Context, s Session, id uuid.UUID, field string) (value, level string, err error)
	AddPass(ctx context.Context, s Session, id uuid.UUID, kind, number string, issued, expires time.Time) (uuid.UUID, error)
	Passes(ctx context.Context, s Session, id uuid.UUID) ([]Pass, error)
	AddDependant(ctx context.Context, s Session, id uuid.UUID, d people.Dependant, from time.Time) (uuid.UUID, error)
	Dependants(ctx context.Context, s Session, id uuid.UUID) ([]Dependant, error)
	AddDocument(ctx context.Context, s Session, id uuid.UUID, typ, title, class, contentType string, content []byte, viewers []string) (uuid.UUID, error)
	Documents(ctx context.Context, s Session, id uuid.UUID) ([]Document, error)
	Download(ctx context.Context, s Session, id, doc uuid.UUID) (contentType string, content []byte, err error)
	ProposeBank(ctx context.Context, s Session, id uuid.UUID, account, bankCode, bankName string) (uuid.UUID, error)
	DecideBank(ctx context.Context, s Session, change uuid.UUID, approve bool, reason string) error
	ConfirmBank(ctx context.Context, s Session, change uuid.UUID, freshPasskey bool) error
	Bank(ctx context.Context, s Session, change uuid.UUID) (BankChange, error)
}

func init() {
}

type idInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
}

type nricInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		NRIC string `json:"nric" doc:"The NRIC or FIN: a letter, seven digits and a letter." example:"S1234567D"`
	}
}

type ir8aInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   IR8AParticulars
}

type secretInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Field  string `path:"field" enum:"nric,bank" doc:"The restricted field." example:"nric"`
}

type secretOutput struct {
	Body struct {
		Value string `json:"value" doc:"The value at the level the caller's roles give: masked or in full." example:"S****567D"`
		Level string `json:"level" enum:"masked,full" doc:"How much of it the caller sees." example:"masked"`
	}
}

type addPassInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		Type    string `json:"pass_type" enum:"employment_pass,s_pass,work_permit,dp_work_permit" doc:"The kind of pass." example:"employment_pass"`
		Number  string `json:"number" doc:"The pass number." example:"EP12345678"`
		Issued  string `json:"issued_on" format:"date" doc:"The day it was issued." example:"2026-01-01"`
		Expires string `json:"expires_on" format:"date" doc:"The day it expires." example:"2027-12-31"`
	}
}

type createdOutput struct {
	Body struct {
		ID string `json:"id" format:"uuid" doc:"The new record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e70"`
	}
}

type passesOutput struct {
	Body struct {
		Passes []Pass `json:"passes" doc:"The person's work passes, soonest to expire first."`
	}
}

type addDependantInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		Relationship   string `json:"relationship" enum:"child,adopted_child,stepchild" doc:"How the child is related." example:"child"`
		BirthDate      string `json:"birth_date,omitempty" format:"date" doc:"The child's date of birth." example:"2025-05-06"`
		AdoptionIntent bool   `json:"adoption_intent,omitempty" doc:"The formal intent to adopt, when there is no date of birth yet." example:"false"`
		Citizenship    string `json:"citizenship" enum:"citizen,permanent_resident,other" doc:"The child's citizenship." example:"citizen"`
		DeclaredOn     string `json:"declared_on" format:"date" doc:"The day the employee made the declaration." example:"2026-01-02"`
		EffectiveFrom  string `json:"effective_from" format:"date" doc:"The day the record starts." example:"2026-01-02"`
	}
}

type dependantsOutput struct {
	Body struct {
		Dependants []Dependant `json:"dependants" doc:"The person's dependants."`
	}
}

type addDocumentInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		Type           string   `json:"doc_type" enum:"contract,kets,letter,pass_copy,certificate,other" doc:"The kind of document." example:"contract"`
		Title          string   `json:"title" minLength:"1" maxLength:"200" doc:"Its title." example:"Employment contract"`
		RetentionClass string   `json:"retention_class" doc:"The retention rule it falls under (a key of the retention schedule)." example:"employment-records"`
		ContentType    string   `json:"content_type" doc:"Its media type." example:"application/pdf"`
		ContentBase64  string   `json:"content_base64" doc:"The file, base64, up to 5 MB." example:"JVBERi0xLjQK"`
		Viewers        []string `json:"viewers" minItems:"1" doc:"The roles who may see it; employee means the person themselves." example:"[\"hr_admin\",\"employee\"]"`
	}
}

type documentsOutput struct {
	Body struct {
		Documents []Document `json:"documents" doc:"The documents the caller may see."`
	}
}

type documentInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Doc    string `path:"doc" format:"uuid" doc:"The document." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e72"`
}

type documentOutput struct {
	Body struct {
		ContentType   string `json:"content_type" doc:"Its media type." example:"application/pdf"`
		ContentBase64 string `json:"content_base64" doc:"The file, base64." example:"JVBERi0xLjQK"`
	}
}

type proposeBankInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		Account  string `json:"account" doc:"The new salary bank account number: 5 to 20 digits." example:"123-456-7890"`
		BankCode string `json:"bank_code,omitempty" pattern:"^[0-9]{4}([0-9]{3})?$" doc:"The bank's Singapore code, 4 digits or 4 and a 3-digit branch. Fyniq looks the bank's BIC up from it, and the approval binds it: give it with the bank's name, or neither." example:"7171"`
		BankName string `json:"bank_name,omitempty" maxLength:"140" doc:"The bank's name, given with its code." example:"DBS Bank"`
	}
}

type changeIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The bank account change." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73"`
}

type decideBankInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The bank account change." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73"`
	Body   struct {
		Approve bool   `json:"approve" doc:"Whether to approve the change." example:"true"`
		Reason  string `json:"reason,omitempty" maxLength:"2000" doc:"Why it is refused: required for a refusal." example:"The number does not match the bank letter."`
	}
}

type bankOutput struct{ Body BankChange }

const maxDocument = 5 << 20

func restrictedError(err error) error {
	var r *people.Refusal
	switch {
	case errors.As(err, &r):
		return Err(r.Code, r.Message)
	case errors.Is(err, ErrUnavailable):
		return Err(CodeUnavailable, "This deployment has no key service or file store for restricted people data yet.")
	}
	return huma.Error500InternalServerError("The restricted data could not be handled.")
}

func (a *API) registerRestricted() {
	// ownDocuments is for the two operations a leaver keeps: a person's documents, which the service limits to what the caller's roles and their own record may see.
	ownDocuments := func(ctx context.Context, cookie, id string) (Session, uuid.UUID, error) {
		s, err := a.signedInOrLeaver(ctx, cookie)
		if err != nil {
			return s, uuid.Nil, err
		}
		pid, err := uuid.Parse(id)
		if err != nil {
			return s, uuid.Nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		return s, pid, nil
	}
	person := func(ctx context.Context, cookie, id string, write bool) (Session, uuid.UUID, error) {
		var s Session
		var pid uuid.UUID
		var err error
		if write {
			s, pid, err = a.writer(ctx, cookie, id)
		} else {
			if s, err = a.signedIn(ctx, cookie); err == nil {
				if pid, err = uuid.Parse(id); err != nil {
					err = Err(CodePersonNotFound, "There is no such person in this organisation.")
				}
			}
		}
		return s, pid, err
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "setPersonNric", Method: "PUT", Path: "/v1/people/{id}/nric", Tags: []string{"People"}, Summary: "Set a person's NRIC or FIN",
		Description: "Seals the NRIC or FIN with the key service and keeps its masked form: it is collected only for statutory filings, never shown in a list, never in a log or an event. The HR officer and the HR administrator. It is audited without the value.",
		Errors:      []int{401, 403, 404, 422, 503},
	}, func(ctx context.Context, in *nricInput) (*struct{}, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, true)
		if err != nil {
			return nil, err
		}
		return nil, restrictedErrOrNil(a.d.Restricted.SetNRIC(ctx, s, id, in.Body.NRIC))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setPersonIR8AParticulars", Method: "PUT", Path: "/v1/people/{id}/ir8a-particulars", Tags: []string{"People"}, Summary: "Set the particulars the IR8A asks of a person",
		Description: "Seals the person's sex, address and, for a non-resident director or seaman, designation with the key service, for the Auto-Inclusion Scheme file only: never shown in a list, never in a log or an event, read only with the NRIC's right and audited. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 422, 503},
	}, func(ctx context.Context, in *ir8aInput) (*struct{}, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, true)
		if err != nil {
			return nil, err
		}
		return nil, restrictedErrOrNil(a.d.Restricted.SetIR8AParticulars(ctx, s, id, in.Body))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPersonSecret", Method: "GET", Path: "/v1/people/{id}/secrets/{field}", Tags: []string{"People"}, Summary: "Read a restricted field",
		Description: "Returns the NRIC or FIN or the salary bank account at the level the caller's roles give (Appendix B): masked for the HR officer, in full for the HR administrator, the Payroll administrator and the Owner as the matrix says, and always in full for the person themselves. Every read, and every refusal, is audited. A field the caller may not see answers as if there were none.",
		Errors:      []int{401, 404, 503},
	}, func(ctx context.Context, in *secretInput) (*secretOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, false)
		if err != nil {
			return nil, err
		}
		v, level, err := a.d.Restricted.Secret(ctx, s, id, in.Field)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &secretOutput{}
		out.Body.Value, out.Body.Level = v, level
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addPersonWorkPass", Method: "POST", Path: "/v1/people/{id}/work-passes", Tags: []string{"People"}, Summary: "Record a work pass",
		Description: "Records a work pass with its number sealed and masked and its dates; the employer is reminded 90, 60 and 30 days before it expires unless a later pass of the same type is recorded. A renewal is a new pass. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *addPassInput) (*createdOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, true)
		if err != nil {
			return nil, err
		}
		issued, err := date("issued_on", in.Body.Issued, true)
		if err != nil {
			return nil, err
		}
		expires, err := date("expires_on", in.Body.Expires, true)
		if err != nil {
			return nil, err
		}
		pid, err := a.d.Restricted.AddPass(ctx, s, id, in.Body.Type, in.Body.Number, *issued, *expires)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &createdOutput{}
		out.Body.ID = pid.String()
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPersonWorkPasses", Method: "GET", Path: "/v1/people/{id}/work-passes", Tags: []string{"People"}, Summary: "A person's work passes",
		Description: "Lists the person's work passes, numbers masked, soonest to expire first. The people team (HR, the Payroll administrator, the Owner). It changes nothing.",
		Errors:      []int{401, 403, 404},
	}, func(ctx context.Context, in *idInput) (*passesOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, false)
		if err != nil {
			return nil, err
		}
		if ok, err := a.peopleTeam(ctx, s); err != nil || !ok {
			if err != nil {
				return nil, err
			}
			return nil, Err(CodeRoleRequired, "Only the people team sees work passes.")
		}
		list, err := a.d.Restricted.Passes(ctx, s, id)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &passesOutput{}
		out.Body.Passes = list
		if list == nil {
			out.Body.Passes = []Pass{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addPersonDependant", Method: "POST", Path: "/v1/people/{id}/dependants", Tags: []string{"People"}, Summary: "Record a dependant for family leave",
		Description: "Records a child with a date of birth, or the formal intent to adopt, for family leave. The person declares their own; the HR administrator records on their behalf. It is audited without the child's date of birth.",
		Errors:      []int{401, 403, 404, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *addDependantInput) (*createdOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, false)
		if err != nil {
			return nil, err
		}
		own, _, err := a.d.People.Relation(ctx, s, id)
		if err != nil {
			return nil, personError(err)
		}
		if !own {
			if err := a.mayCreate(ctx, s); err != nil {
				return nil, err
			}
		}
		birth, err := date("birth_date", in.Body.BirthDate, false)
		if err != nil {
			return nil, err
		}
		declared, err := date("declared_on", in.Body.DeclaredOn, true)
		if err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.EffectiveFrom, true)
		if err != nil {
			return nil, err
		}
		dep := people.Dependant{Relationship: in.Body.Relationship, BirthDate: birth, AdoptionIntent: in.Body.AdoptionIntent, Citizenship: in.Body.Citizenship, DeclaredOn: *declared}
		did, err := a.d.Restricted.AddDependant(ctx, s, id, dep, *from)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &createdOutput{}
		out.Body.ID = did.String()
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPersonDependants", Method: "GET", Path: "/v1/people/{id}/dependants", Tags: []string{"People"}, Summary: "A person's dependants",
		Description: "Lists the person's dependants at the level the caller's roles give (Appendix B: Dependants): the HR officer in full, the person themselves their own, everyone else nothing. Every read is audited. A line manager is told there are none to see.",
		Errors:      []int{401, 404},
	}, func(ctx context.Context, in *idInput) (*dependantsOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, false)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Restricted.Dependants(ctx, s, id)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &dependantsOutput{}
		out.Body.Dependants = list
		if list == nil {
			out.Body.Dependants = []Dependant{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addPersonDocument", Method: "POST", Path: "/v1/people/{id}/documents", Tags: []string{"People"}, Summary: "Add a document to a person's file",
		Description: "Stores a document (contract, KETs, letter, pass copy, certificate) of up to 5 MB as write-once evidence, with a retention class and the roles who may see it. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 413, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *addDocumentInput) (*createdOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, true)
		if err != nil {
			return nil, err
		}
		content, err := base64.StdEncoding.DecodeString(in.Body.ContentBase64)
		if err != nil || len(content) == 0 {
			return nil, Err(CodePersonInvalid, "content_base64 is not base64 of a file.")
		}
		if len(content) > maxDocument {
			return nil, Err(CodePersonInvalid, "The file is larger than 5 MB: attach a smaller one.")
		}
		did, err := a.d.Restricted.AddDocument(ctx, s, id, in.Body.Type, in.Body.Title, in.Body.RetentionClass, in.Body.ContentType, content, in.Body.Viewers)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &createdOutput{}
		out.Body.ID = did.String()
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPersonDocuments", Method: "GET", Path: "/v1/people/{id}/documents", Tags: []string{"People"}, Summary: "A person's documents",
		Description: "Lists the person's documents that the caller's roles may see: a role sees a document only when it is named a viewer, and the person sees theirs when employee is named. It changes nothing.",
		Errors:      []int{401, 404},
	}, func(ctx context.Context, in *idInput) (*documentsOutput, error) {
		s, id, err := ownDocuments(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Restricted.Documents(ctx, s, id)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &documentsOutput{}
		out.Body.Documents = list
		if list == nil {
			out.Body.Documents = []Document{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPersonDocument", Method: "GET", Path: "/v1/people/{id}/documents/{doc}", Tags: []string{"People"}, Summary: "A document's content",
		Description: "Returns a document's file to a caller who may see it, and audits the read. A document the caller may not see answers as if it did not exist.",
		Errors:      []int{401, 404, 503},
	}, func(ctx context.Context, in *documentInput) (*documentOutput, error) {
		s, id, err := ownDocuments(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		doc, err := uuid.Parse(in.Doc)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such document.")
		}
		ct, content, err := a.d.Restricted.Download(ctx, s, id, doc)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &documentOutput{}
		out.Body.ContentType, out.Body.ContentBase64 = ct, base64.StdEncoding.EncodeToString(content)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "proposeBankAccountChange", Method: "POST", Path: "/v1/people/{id}/bank-account-changes", Tags: []string{"People"}, Summary: "Propose a new salary bank account",
		Description: "An employee proposes a new salary bank account for themselves. It takes effect only when an HR administrator, the Payroll administrator or the Owner who is not the proposer approves it and the employee confirms it with a passkey. It is audited; the account is never in an event.",
		Errors:      []int{401, 404, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *proposeBankInput) (*createdOutput, error) {
		s, id, err := person(ctx, in.Cookie, in.ID, false)
		if err != nil {
			return nil, err
		}
		cid, err := a.d.Restricted.ProposeBank(ctx, s, id, in.Body.Account, in.Body.BankCode, in.Body.BankName)
		if err != nil {
			return nil, restrictedError(err)
		}
		out := &createdOutput{}
		out.Body.ID = cid.String()
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideBankAccountChange", Method: "POST", Path: "/v1/bank-account-changes/{id}/decision", Tags: []string{"People"}, Summary: "Approve or refuse a bank account change",
		Description: "Approves or refuses an open change. Only the HR administrator, the Payroll administrator or the Owner, never the proposer nor the person whose account it is. A refusal says why. It is audited.",
		Errors:      []int{401, 403, 404, 422},
	}, func(ctx context.Context, in *decideBankInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		cid, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such bank account change.")
		}
		return nil, restrictedErrOrNil(a.d.Restricted.DecideBank(ctx, s, cid, in.Body.Approve, in.Body.Reason))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "confirmBankAccountChange", Method: "POST", Path: "/v1/bank-account-changes/{id}/confirm", Tags: []string{"People"}, Summary: "Confirm an approved bank account change",
		Description: "The employee whose account it is confirms an approved change after a passkey confirmation in the last five minutes (POST /v1/session/step-up); the account then takes effect and the payroll approvers are noticed. It is audited.",
		Errors:      []int{401, 403, 404, 422},
	}, func(ctx context.Context, in *changeIDInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirming a salary bank account needs a passkey confirmation in the last five minutes.")
		}
		cid, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such bank account change.")
		}
		return nil, restrictedErrOrNil(a.d.Restricted.ConfirmBank(ctx, s, cid, true))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getBankAccountChange", Method: "GET", Path: "/v1/bank-account-changes/{id}", Tags: []string{"People"}, Summary: "A bank account change",
		Description: "Returns a change with its account masked and where it is. The person it concerns and the approvers (HR administrator, Payroll administrator, Owner) read it; anyone else is told there is none. It changes nothing.",
		Errors:      []int{401, 404},
	}, func(ctx context.Context, in *changeIDInput) (*bankOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		cid, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such bank account change.")
		}
		v, err := a.d.Restricted.Bank(ctx, s, cid)
		if err != nil {
			return nil, restrictedError(err)
		}
		return &bankOutput{Body: v}, nil
	})
	_ = authz.Owner
}

func restrictedErrOrNil(err error) error {
	if err == nil {
		return nil
	}
	return restrictedError(err)
}
