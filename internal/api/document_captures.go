package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	capdom "github.com/cognizhi/fyniq/internal/domain/capture"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/pdftext"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the document inbox (FR-DOC-12, FR-DOC-14, FR-AP-04, FR-AP-12; p1-document-capture)
//
// A file is captured by upload, paste or from accepted mail, kept in the staging store under its own key, and read by the model on a person's
// request: the reading is the agent document_capture's, with no tools, a strict output schema and screening before and after (AI-18, AI-28), kept
// as data labelled the model's and never deciding a figure. A person who may enter bills or claims makes the draft from the reading as they
// corrected it, through the same functions a person's own draft uses, and the file becomes the draft's evidence. The AI holds no write.

// The codes of the document inbox's refusals.
const (
	CodeCaptureState     = "CAP-01" // the document is not in a state that allows the action
	CodeCaptureDuplicate = "CAP-02" // the bill duplicates one already entered
	CodeCaptureType      = "CAP-03" // the file is not one the model reads (a PDF or a photo)
	CodeCaptureReading   = "CAP-04" // the document could not be read, as the answer says
	CodeCaptureAdopted   = "CAP-05" // the mail's file is in the inbox already
)

// CaptureDraft is the draft a person made of a captured document.
type CaptureDraft struct {
	Kind string `json:"kind" enum:"bill,claim" doc:"What was drafted." example:"bill"`
	ID   string `json:"id" format:"uuid" doc:"The draft bill or claim." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f31"`
}

// CaptureSupplier is the supplier contact a reading's supplier matches: by its UEN, else by its name.
type CaptureSupplier struct {
	ID       string           `json:"id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f31"`
	Name     string           `json:"name" doc:"Its legal name." example:"Acme Software Pte Ltd"`
	By       string           `json:"by" enum:"uen,name" doc:"How it matched." example:"uen"`
	Defaults *CaptureDefaults `json:"defaults,omitempty" doc:"What this supplier's bills have usually used, suggested for the draft (FR-AP-12)."`
}

// CaptureDefaults are a supplier's defaults, suggested from its bills and kept on its contact once a person confirms them (FR-AP-12).
type CaptureDefaults struct {
	AccountID      string `json:"account_id,omitempty" format:"uuid" doc:"The account most of the supplier's bill lines cost: its contact's default when it has one, else the most used." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
	GSTCode        string `json:"gst_code,omitempty" doc:"The GST code most of its lines use: its contact's default when it has one, else the most used." example:"TX"`
	FromBills      int    `json:"from_bills" doc:"How many of the supplier's bills the suggestion rests on; 0 when only the contact's own defaults are shown." example:"5"`
	SavedOnContact bool   `json:"saved_on_contact" doc:"true when the contact already holds these as its defaults." example:"false"`
}

// DocumentCapture is one file of the document inbox.
type DocumentCapture struct {
	ID         string    `json:"id" format:"uuid" doc:"The capture's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Source     string    `json:"source" enum:"upload,paste,email" doc:"How it came in." example:"upload"`
	FileName   string    `json:"file_name" doc:"As sent: data, never a path." example:"AU-1182.pdf"`
	MediaType  string    `json:"media_type" doc:"Its type, decided from its content." example:"application/pdf"`
	Size       int64     `json:"size_bytes" doc:"Its size as stored." example:"48211"`
	SHA256     string    `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Status     string    `json:"status" enum:"received,reading,read,duplicate,drafted,dismissed,failed" doc:"received: kept, not read yet; reading: the model is reading it; read: its reading is ready to check; duplicate: the same file is in the inbox already; drafted: a person made a draft of it; dismissed: no draft will be made; failed: it could not be read, as note says." example:"read"`
	Kind       string    `json:"kind,omitempty" enum:"bill,receipt,sales_invoice,statement,contract,other" doc:"What the reading says it is." example:"bill"`
	CapturedBy string    `json:"captured_by" doc:"Who captured it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	CreatedAt  time.Time `json:"created_at" doc:"When it was captured." example:"2026-11-20T09:00:00Z"`
	Own        bool      `json:"own" doc:"true when it arrived at the caller's own email-in address: only they see it." example:"false"`
	NRIC       bool      `json:"nric_flagged" doc:"true when its text shows an NRIC or FIN in full (FR-DOC-09): it is never sent to a model, so it is not read; mask it and capture it again, or type the bill in." example:"false"`
	MessageID  string    `json:"message_id,omitempty" format:"uuid" doc:"The accepted mail it came from." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f33"`
	// What the model read, apart from the file and labelled so: it never decides a figure, a GST eligibility or a posting.
	ReadingIs     string             `json:"reading_is,omitempty" doc:"Always the model's reading: untrusted data to check against the document, never a figure Fyniq decided." example:"the model's reading"`
	Reading       *capdom.Reading    `json:"reading,omitempty" doc:"What the model read, each field with its confidence; absent until it is read."`
	Confidence    map[string]float64 `json:"confidence,omitempty" doc:"Each field's confidence after the platform's checks, which only ever lower it."`
	LowConfidence []string           `json:"low_confidence,omitempty" nullable:"false" doc:"The fields shown that the model or a check doubts, to highlight for the person who reviews the draft." example:"[\"total\"]"`
	Checks        []capdom.Check     `json:"checks,omitempty" nullable:"false" doc:"What the platform found in the reading: arithmetic, dates, the GST number's form, the IRAS tax-invoice particulars it lacks."`
	ReadAt        *time.Time         `json:"read_at,omitempty" doc:"When it was last read." example:"2026-11-20T09:00:05Z"`
	Model         string             `json:"model,omitempty" doc:"The model that read it." example:"claude-opus-5-5"`
	Readings      int                `json:"readings" doc:"How many times it was read." example:"1"`
	Supplier      *CaptureSupplier   `json:"supplier,omitempty" doc:"The supplier contact the reading's supplier matches, when one does."`
	DuplicateOf   string             `json:"duplicate_of,omitempty" format:"uuid" doc:"The capture of the same file, for a duplicate." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f2f"`
	Draft         *CaptureDraft      `json:"draft,omitempty" doc:"The draft a person made of it."`
	DismissReason string             `json:"dismiss_reason,omitempty" doc:"Why it was dismissed." example:"Not ours"`
	Note          string             `json:"note,omitempty" doc:"Why it failed, in plain words." example:"Only a PDF or a photo can be read: type the bill in"`
	Version       int                `json:"version" doc:"Its version; it makes the ETag." example:"1"`
}

// DocumentCapturePage is a page of the document inbox, newest first.
type DocumentCapturePage struct {
	Items      []DocumentCapture `json:"items" nullable:"false" doc:"The files, newest first."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// CapturePath names a capture.
type CapturePath struct {
	CaptureID string `path:"capture_id" format:"uuid" doc:"The capture's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
}

// CaptureUpload is a file to capture.
type CaptureUpload struct {
	File   huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"The file: a PDF or a photo of a bill or receipt (PNG, JPEG, WebP; HEIC is converted), up to 25 MB. Its type is decided from its content."`
	Source string        `form:"source" required:"false" enum:"upload,paste" doc:"upload (the default) or paste." example:"upload"`
}

// CaptureFromMessage adopts a file of accepted mail.
type CaptureFromMessage struct {
	MessageID string `json:"message_id" format:"uuid" doc:"The accepted message." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f33"`
	FileID    string `json:"file_id" format:"uuid" doc:"Its attachment." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f34"`
}

// CaptureDismissal says why no draft will be made.
type CaptureDismissal struct {
	Reason string `json:"reason,omitempty" maxLength:"500" doc:"Why, in a few words." example:"Not ours"`
}

// CaptureBillDraft is a bill as the person corrected it from the reading.
type CaptureBillDraft struct {
	BillDraft
	DuplicateReason string `json:"duplicate_reason,omitempty" maxLength:"500" doc:"Why the bill is entered though it duplicates one (the same supplier, number and total within 60 days): the audit trail keeps it (FR-AP-12). Without it, a duplicate is refused." example:"The supplier sent a corrected invoice"`
	SaveDefaults    bool   `json:"save_defaults,omitempty" doc:"Keep the first line's account and GST code as the supplier's defaults on its contact (FR-AP-12), for the bills made from it from now on; needs the right to change contacts, else it is refused, and a default the contact already has is left." example:"true"`
}

// CaptureDrafted is what a draft made of a captured document.
type CaptureDrafted struct {
	Capture DocumentCapture `json:"capture" doc:"The capture, now drafted."`
	Draft   CaptureDraft    `json:"draft" doc:"The draft bill or claim, with the file attached to it as its evidence."`
}

type (
	captureUploadInput struct {
		KeyHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[CaptureUpload]
	}
	captureFromMessageInput struct {
		KeyHeader
		PreferHeader
		Body CaptureFromMessage
	}
	captureListInput struct {
		PageQuery
		Status string `query:"status" enum:"received,reading,read,duplicate,drafted,dismissed,failed" required:"false" doc:"Only captures in this status." example:"read"`
	}
	captureReadInput struct {
		CapturePath
		IfMatchHeader
	}
	captureDismissInput struct {
		CapturePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body CaptureDismissal
	}
	captureBillInput struct {
		CapturePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body CaptureBillDraft
	}
	captureClaimInput struct {
		CapturePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body ExpenseClaimDraft
	}
	captureOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The capture's version, for If-Match."`
		Location string `header:"Location" doc:"The capture's URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     DocumentCapture
	}
	captureDraftedOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The capture's new version."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     CaptureDrafted
	}
	capturePageOutput struct{ Body DocumentCapturePage }
	captureFileOutput struct {
		ContentType        string `header:"Content-Type" doc:"The file's type."`
		ContentDisposition string `header:"Content-Disposition" doc:"attachment, with the file's name."`
		CacheControl       string `header:"Cache-Control" doc:"no-store."`
		NoSniff            string `header:"X-Content-Type-Options" doc:"nosniff: the file is untrusted and is never shown inline."`
		Body               []byte
	}
)

func docCaptureETag(c storage.DocumentCapture) string { return etag(c.Version, c.Status) }

// captureOf is a capture as the API shows it; supplier is the contact its reading matches, nil when none.
func (a *API) captureOf(c storage.DocumentCapture, viewer uuid.UUID, supplier *CaptureSupplier) DocumentCapture {
	out := DocumentCapture{ID: c.ID.String(), Source: c.Source, FileName: c.FileName, MediaType: c.MediaType, Size: c.Size, SHA256: c.SHA256, Status: c.Status, Kind: c.Kind,
		CapturedBy: c.CapturedBy, CreatedAt: c.CreatedAt.UTC(), Own: c.OwnerUserID != nil && *c.OwnerUserID == viewer, NRIC: c.NRICFlagged, ReadAt: c.ReadAt, Model: c.ReadingModel, Readings: c.Readings,
		Supplier: supplier, DismissReason: c.DismissReason, Note: c.LastError, Version: c.Version}
	if c.MessageID != nil {
		out.MessageID = c.MessageID.String()
	}
	if c.DuplicateOf != nil {
		out.DuplicateOf = c.DuplicateOf.String()
	}
	if c.DraftID != nil {
		out.Draft = &CaptureDraft{Kind: c.DraftKind, ID: c.DraftID.String()}
	}
	if c.ReadAt != nil {
		t := c.ReadAt.UTC()
		out.ReadAt = &t
	}
	if len(c.Reading) > 0 {
		var r capdom.Reading
		if json.Unmarshal(c.Reading, &r) == nil {
			out.ReadingIs, out.Reading = "the model's reading", &r
		}
		var res capdom.Result
		if json.Unmarshal(c.Checks, &res) == nil {
			out.Confidence, out.LowConfidence, out.Checks = res.Fields, res.Low, res.Checks
		}
	}
	return out
}

// supplierOf is the active supplier contact a reading's supplier matches: its UEN first, then its name without its company suffix.
func supplierOf(ctx context.Context, u storage.UnitOfWork, c storage.DocumentCapture) (*CaptureSupplier, error) {
	if len(c.Reading) == 0 {
		return nil, nil
	}
	var r capdom.Reading
	if json.Unmarshal(c.Reading, &r) != nil {
		return nil, nil
	}
	value := func(t capdom.Text) string {
		if t.Value == nil {
			return ""
		}
		return strings.TrimSpace(*t.Value)
	}
	uen, name := strings.ToUpper(strings.ReplaceAll(value(r.SupplierUEN), " ", "")), value(r.Supplier)
	for _, q := range []string{uen, capdom.Name(name)} { // the name without its company suffix and punctuation, which a contact's name may spell differently
		if q == "" {
			continue
		}
		hits, err := u.Contacts().Search(ctx, q, 25)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			if !h.Vendor || !h.Active {
				continue
			}
			by := ""
			switch {
			case uen != "" && strings.ToUpper(strings.ReplaceAll(h.UENOrTaxID, " ", "")) == uen:
				by = "uen"
			case name != "" && capdom.Name(h.LegalName) == capdom.Name(name):
				by = "name"
			}
			if by == "" {
				continue
			}
			defaults, err := supplierDefaults(ctx, u, h)
			return &CaptureSupplier{ID: h.ID.String(), Name: h.LegalName, By: by, Defaults: defaults}, err
		}
	}
	return nil, nil
}

// supplierDefaults are what the draft is suggested for a supplier (FR-AP-12): the contact's own default account and GST code when it has them,
// else the account and GST code most of the lines of its bills use. Nil when there is neither.
func supplierDefaults(ctx context.Context, u storage.UnitOfWork, c storage.Contact) (*CaptureDefaults, error) {
	out := &CaptureDefaults{}
	if c.DefaultAccountID != nil {
		out.AccountID = c.DefaultAccountID.String()
	}
	out.GSTCode = c.DefaultGSTCode
	docs, err := u.Documents().OfContact(ctx, c.ID, 200)
	if err != nil {
		return nil, err
	}
	accounts, codes := map[string]int{}, map[string]int{}
	for _, d := range docs {
		if d.Type != storage.DocumentBill || d.Status != storage.DocumentPosted {
			continue
		}
		var p struct {
			Lines []struct {
				AccountID uuid.UUID `json:"account_id"`
				GSTCode   string    `json:"gst_code"`
			} `json:"lines"`
		}
		if json.Unmarshal(d.Payload, &p) != nil || len(p.Lines) == 0 {
			continue
		}
		out.FromBills++
		for _, l := range p.Lines {
			accounts[l.AccountID.String()]++
			codes[l.GSTCode]++
		}
	}
	most := func(m map[string]int) string {
		best := ""
		for k, n := range m {
			if k != "" && (best == "" || n > m[best] || n == m[best] && k < best) {
				best = k
			}
		}
		return best
	}
	saved := out.AccountID != "" || out.GSTCode != ""
	if out.AccountID == "" {
		out.AccountID = most(accounts)
	}
	if out.GSTCode == "" {
		out.GSTCode = most(codes)
	}
	if out.AccountID == "" && out.GSTCode == "" {
		return nil, nil
	}
	out.SavedOnContact = saved && c.DefaultAccountID != nil && c.DefaultGSTCode != ""
	return out, nil
}

// visibleCapture is a capture the caller may see: the organisation's, or one that arrived at their own email-in address; any other is not found.
func visibleCapture(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, lock bool) (storage.DocumentCapture, error) {
	get := u.DocumentCaptures().Get
	if lock {
		get = u.DocumentCaptures().Lock
	}
	c, err := get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && c.OwnerUserID != nil && *c.OwnerUserID != p.UserID) {
		return storage.DocumentCapture{}, problem(CodeNotFound, "No such document in your inbox.", "List the inbox to find it.")
	}
	return c, err
}

// captureKey is where a captured file is kept in the staging store.
func captureKey(org uuid.UUID) string {
	return "capture/" + org.String() + "/" + uuid.Must(uuid.NewV7()).String()
}

// mediaReadable is whether the model reads a file of this type: the text of a PDF, a plain text, CSV or email file. A photo is not: its pixels cannot be
// masked before the model reads them (AI-02; the owner's decision of 20 Nov 2026).
func mediaReadable(mt string) bool {
	return mt == evidence.PDF || mt == evidence.Text || mt == evidence.CSV || mt == evidence.EML
}

// ReadingGrace is how long a document may be Reading before the next ask ends it as Failed.
const ReadingGrace = 10 * time.Minute

// maxReadText is the most of a document's text the model is given.
const maxReadText = 60000

// maskedTextOf is the text of a file as the model reads it, with its bank account numbers, IBANs and NRIC or FIN numbers hidden first (AI-02, the owner's
// decision of 20 Nov 2026), and how many things were hidden. A PDF whose text Fyniq cannot extract is refused: it cannot be checked, so it does not go to the model.
func maskedTextOf(mediaType string, data []byte) (string, int, error) {
	text := string(data)
	if mediaType == evidence.PDF {
		var err error
		if text, err = pdftext.Text(data); err != nil || len(strings.TrimSpace(text)) < 20 {
			return "", 0, problem(CodeCaptureType, "Fyniq cannot read this PDF's text, so it cannot hide the bank and identity numbers on it before the model sees it, and it is not read.",
				"Type the bill in by hand, or capture its email or a text copy")
		}
	}
	if len(text) > maxReadText {
		text = text[:maxReadText]
	}
	masked, n := capdom.MaskText(text)
	return masked, n, nil
}

// capturing adds file, already checked as any upload is, to the inbox in u; a file the inbox holds already is a duplicate of it.
func (a *API) capturing(ctx context.Context, u storage.UnitOfWork, p authn.Principal, file evidence.File, source, key string, message, inboundFile, owner *uuid.UUID) (storage.DocumentCapture, error) {
	n := storage.NewDocumentCapture{Source: source, FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, BlobKey: key,
		CapturedBy: uploader(ctx), Status: storage.CaptureReceived, MessageID: message, InboundFileID: inboundFile, OwnerUserID: owner, At: a.deps.Clock.Now(), NRICFlagged: file.NRIC}
	if inboundFile != nil {
		if _, ok, err := u.DocumentCaptures().OfInboundFile(ctx, *inboundFile); err != nil {
			return storage.DocumentCapture{}, err
		} else if ok {
			return storage.DocumentCapture{}, problem(CodeCaptureAdopted, "That mail's file is in the document inbox already.", "Open it from the inbox")
		}
	}
	if earlier, ok, err := u.DocumentCaptures().OfHash(ctx, p.UserID, file.SHA256); err != nil {
		return storage.DocumentCapture{}, err
	} else if ok {
		n.Status, n.DuplicateOf = storage.CaptureDuplicate, &earlier.ID
	}
	return u.DocumentCaptures().Capture(ctx, n)
}

func registerDocumentCaptures(a *API) {
	formCodes := []string{CodeInvalid, CodeMalformed}
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeCaptureState, CodeInvalid}

	register(a, authz.CaptureDocument, huma.Operation{
		OperationID: "captureDocument", Method: http.MethodPost, Path: "/v1/captures", Tags: []string{"Document inbox"}, DefaultStatus: http.StatusCreated,
		Summary: "Capture a bill or a receipt",
		Description: "Adds a file to the document inbox (FR-DOC-12, FR-DOC-14): an upload or a paste, checked as any upload is (its type from its content, a malware " +
			"scan, image metadata stripped, FR-DOC-08), kept in the staging store with its hash. The same file already in the inbox is a duplicate of it, " +
			"with a link, and is not read again. Nothing is read or drafted here: readCapture asks the model, and draftBillFromCapture or draftClaimFromCapture " +
			"makes a draft. Side effects: the capture and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeUnavailable, CodeMalformed, CodeTooLarge, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid}, locks...),
		func(ctx context.Context, in *captureUploadInput) (*captureOutput, error) {
			p, _ := authn.From(ctx)
			up := in.RawBody.Data()
			if up == nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the file in the form field file"}})
			}
			source := up.Source
			if source == "" {
				source = storage.CaptureUpload
			}
			file, _, err := a.upload(ctx, &EvidenceUpload{File: up.File, EvidenceType: "receipt", Source: source})
			if err != nil {
				return nil, err
			}
			return a.captureFile(ctx, p, file, source, in.IdempotencyKey, in.Prefer, nil, nil, nil)
		})

	// huma describes the file as binary but takes no example for it from a tag.
	if op := a.OpenAPI().Paths["/v1/captures"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.CaptureFromMessage, huma.Operation{
		OperationID: "captureFromMessage", Method: http.MethodPost, Path: "/v1/captures/from-message", Tags: []string{"Document inbox"}, DefaultStatus: http.StatusCreated,
		Summary: "Capture a file of accepted mail",
		Description: "Adopts an attachment of accepted mail into the document inbox (FR-DOC-12, FR-INT-22): from the organisation's email-in address, or from the " +
			"caller's own. A quarantined or rejected message is never captured, and a member's own mail is only theirs to adopt; any other is EML-10. The file " +
			"is checked again as any upload is, and each attachment is adopted once. Side effects: the capture and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeUnavailable, CodeMessageFile, CodeCaptureAdopted, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid}, locks...),
		func(ctx context.Context, in *captureFromMessageInput) (*captureOutput, error) {
			p, _ := authn.From(ctx)
			messageID, err := parseID("body.message_id", in.Body.MessageID)
			if err != nil {
				return nil, err
			}
			fileID, err := parseID("body.file_id", in.Body.FileID)
			if err != nil {
				return nil, err
			}
			if a.deps.Staging == nil || a.deps.Scanner == nil {
				return nil, problem(CodeUnavailable, "Files cannot be captured here: no file store or malware scanner is configured.", "Ask your administrator to configure them (FR-DOC-01).")
			}
			var data []byte
			var name string
			var owner *uuid.UUID
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				f, who, err := adoptableFile(ctx, u, p.UserID, messageID, fileID)
				if err != nil {
					return err
				}
				r, err := a.deps.Staging.Get(ctx, f.BlobKey)
				if err != nil {
					logFailure(ctx, err)
					return problem(CodeMessageFile, "The mail's file is no longer kept.", "Ask for it to be sent again.")
				}
				defer r.Close()
				if data, err = io.ReadAll(io.LimitReader(r, evidence.MaxBytes+1)); err != nil {
					return err
				}
				name, owner = f.FileName, who
				return nil
			}); err != nil {
				return nil, err
			}
			file, _, err := a.upload(ctx, &EvidenceUpload{File: huma.FormFile{File: fileCloser{bytes.NewReader(data)}, Filename: name, Size: int64(len(data))}, EvidenceType: "receipt"})
			if err != nil {
				return nil, err
			}
			return a.captureFile(ctx, p, file, storage.CaptureEmail, in.IdempotencyKey, in.Prefer, &messageID, &fileID, owner)
		})

	register(a, authz.ListCaptures, huma.Operation{
		OperationID: "listCaptures", Method: http.MethodGet, Path: "/v1/captures", Tags: []string{"Document inbox"},
		Summary: "List the document inbox",
		Description: "The captured files the caller may see, newest first: the organisation's and the caller's own, never another member's (FR-DOC-14), each with " +
			"its status and, once read, the model's reading apart from the file. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *captureListInput) (*capturePageOutput, error) {
		p, _ := authn.From(ctx)
		out := DocumentCapturePage{Items: []DocumentCapture{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			before := uuid.Nil
			if in.Cursor != "" {
				after, err := decodeCursor(in.Cursor)
				if err != nil {
					return err
				}
				if before, err = uuid.Parse(after); err != nil {
					return badCursor()
				}
			}
			cs, err := u.DocumentCaptures().List(ctx, p.UserID, in.Status, before, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(cs) > in.PageSize {
				cs = cs[:in.PageSize]
				out.NextCursor = encodeCursor(cs[len(cs)-1].ID.String())
			}
			for _, c := range cs {
				supplier, err := supplierOf(ctx, u, c)
				if err != nil {
					return err
				}
				out.Items = append(out.Items, a.captureOf(c, p.UserID, supplier))
			}
			return nil
		})
		return &capturePageOutput{Body: out}, err
	})

	register(a, authz.GetCapture, huma.Operation{
		OperationID: "getCapture", Method: http.MethodGet, Path: "/v1/captures/{capture_id}", Tags: []string{"Document inbox"},
		Summary: "Get a captured document",
		Description: "A captured file with the model's reading, its confidences and the platform's checks, apart from the file, and the supplier contact it matches. " +
			"The reading is data to check against the document: it never decides a figure or a GST eligibility. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *CapturePath) (*captureOutput, error) {
		id, err := parseID("capture_id", in.CaptureID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out DocumentCapture
		var tag string
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := visibleCapture(ctx, u, p, id, false)
			if err != nil {
				return err
			}
			supplier, err := supplierOf(ctx, u, c)
			out, tag = a.captureOf(c, p.UserID, supplier), docCaptureETag(c)
			return err
		})
		return &captureOutput{Status: http.StatusOK, ETag: tag, Body: out}, err
	})

	register(a, authz.GetCaptureFile, exempt(huma.Operation{
		OperationID: "getCaptureFile", Method: http.MethodGet, Path: "/v1/captures/{capture_id}/file", Tags: []string{"Document inbox"},
		Summary: "Download a captured file",
		Description: "The file as captured, as an attachment named file_name and of its type, never shown inline and never cached: it is untrusted data (AI-18), shown beside the " +
			"reading so a person can check it. Read-only; no side effects.",
		Responses: map[string]*huma.Response{"200": {Description: "The file itself, of its captured type, as an attachment with its name.",
			Content: map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary", Examples: []any{"%PDF-1.7 AU-100"}}}}}},
	}, "cursor", "It is one file, not a list, so there is nothing to page through and nothing to resume."), []string{CodeNotFound, CodeUnavailable}, func(ctx context.Context, in *CapturePath) (*captureFileOutput, error) {
		id, err := parseID("capture_id", in.CaptureID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var c storage.DocumentCapture
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			c, err = visibleCapture(ctx, u, p, id, false)
			return err
		}); err != nil {
			return nil, err
		}
		if a.deps.Staging == nil {
			return nil, problem(CodeUnavailable, "No file store is configured here.", "Ask your administrator to configure one (FR-DOC-01).")
		}
		r, err := a.deps.Staging.Get(ctx, c.BlobKey)
		if err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The file is no longer kept.", "Capture it again.")
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, evidence.MaxBytes+1))
		if err != nil {
			return nil, err
		}
		return &captureFileOutput{ContentType: c.MediaType, ContentDisposition: "attachment; filename=\"" + safeName(c.FileName) + "\"", CacheControl: "no-store", NoSniff: "nosniff", Body: data}, nil
	})

	register(a, authz.ReadCapture, exempt(exempt(huma.Operation{
		OperationID: "readCapture", Method: http.MethodPost, Path: "/v1/captures/{capture_id}/read", Tags: []string{"Document inbox"},
		Summary: "Have the model read a captured document",
		Description: "Asks the model to read the file (FR-DOC-12, AI-28), as the caller, through the route of the agent document_capture, the AI switch and the budgets " +
			"applying (AI-74, AI-13): the file goes as a PDF or an image part with no tools and a strict output schema, its text screened first when it has any and the " +
			"answer screened after (AI-18). The reading comes back with each field's confidence and the platform's checks, as data labelled the model's, and nothing " +
			"is drafted or changed. Only a PDF or a photo is read; a document read already is read again only on request. A failure is a status with why, never a retry " +
			"loop. Side effects: the model call's record in the AI activity log and the capture's reading with its audit event. Needs If-Match with the capture's ETag.",
	}, "Idempotency-Key", "A reading is a new model call whichever way it ends; If-Match makes a repeat of the same request refuse as stale (FR-INT-08)."),
		"Prefer", "A preview would have to call the model, which is the one thing a reading does; there is nothing else to check first (FR-INT-14)."),
		append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeCaptureState, CodeCaptureType, CodeCaptureReading, CodeUnavailable}, locks...),
		func(ctx context.Context, in *captureReadInput) (*captureOutput, error) {
			id, err := parseID("capture_id", in.CaptureID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			c, err := a.readDocument(ctx, p, id, in.IfMatch)
			if err != nil {
				return nil, err
			}
			var out DocumentCapture
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				supplier, err := supplierOf(ctx, u, c)
				out = a.captureOf(c, p.UserID, supplier)
				return err
			})
			return &captureOutput{Status: http.StatusOK, ETag: docCaptureETag(c), Body: out}, err
		})

	register(a, authz.DismissCapture, huma.Operation{
		OperationID: "dismissCapture", Method: http.MethodPost, Path: "/v1/captures/{capture_id}/dismiss", Tags: []string{"Document inbox"},
		Summary: "Dismiss a captured document",
		Description: "Ends a document nobody will draft, with a reason (FR-DOC-14): a dismissal is a status, and the file is kept as captured files are. A drafted or " +
			"dismissed document is final. Side effects: the status and its audit event. Needs If-Match with the capture's ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append(writes, locks...), func(ctx context.Context, in *captureDismissInput) (*captureOutput, error) {
		id, err := parseID("capture_id", in.CaptureID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DocumentCapture, error) {
			c, err := visibleCapture(ctx, u, p, id, true)
			if err != nil {
				return DocumentCapture{}, err
			}
			if err := checkIfMatch(in.IfMatch, docCaptureETag(c)); err != nil {
				return DocumentCapture{}, err
			}
			if !slices.Contains([]string{storage.CaptureReceived, storage.CaptureReading, storage.CaptureRead, storage.CaptureFailed, storage.CaptureDuplicate}, c.Status) {
				return DocumentCapture{}, problem(CodeCaptureState, "A document that is "+c.Status+" cannot be dismissed.", "Only a document waiting in the inbox is dismissed")
			}
			d, err := u.DocumentCaptures().Dismiss(ctx, c.ID, c.Version, p.UserID, strings.TrimSpace(in.Body.Reason), a.deps.Clock.Now())
			if err != nil {
				return DocumentCapture{}, err
			}
			return a.captureOf(d, p.UserID, nil), nil
		})
		if err != nil {
			return nil, err
		}
		return &captureOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.DraftBillFromCapture, huma.Operation{
		OperationID: "draftBillFromCapture", Method: http.MethodPost, Path: "/v1/captures/{capture_id}/bill", Tags: []string{"Document inbox"}, DefaultStatus: http.StatusCreated,
		Summary: "Make a draft bill from a read document",
		Description: "Saves a draft bill from a read document, from the bill as the caller corrected it (FR-AP-04, FR-AP-12): the same draft, checks and audit as saveBill, " +
			"made as the caller, with the file attached to it as its evidence and the capture marked drafted. Nothing is posted: the bill follows its approval policy " +
			"when a person posts it (AI-30), and its input tax stays as the caller's facts decide (FR-AP-20), never from an extracted amount (G4-029). A bill that " +
			"duplicates one (the same supplier, number and total within 60 days) is refused unless duplicate_reason says why (CAP-02). Side effects: the draft bill, its " +
			"evidence, the capture's status and their audit events. Needs If-Match with the capture's ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"runs every check and saves nothing.",
	}, append(append([]string{CodeUnavailable, CodeCaptureDuplicate}, writes...), formCodes...), func(ctx context.Context, in *captureBillInput) (*captureDraftedOutput, error) {
		return a.draftFromCapture(ctx, in.CaptureID, in.IfMatch, in.IdempotencyKey, in.Prefer, in.Body, storage.CaptureDraftBill)
	})

	register(a, authz.DraftClaimFromCapture, huma.Operation{
		OperationID: "draftClaimFromCapture", Method: http.MethodPost, Path: "/v1/captures/{capture_id}/claim", Tags: []string{"Document inbox"}, DefaultStatus: http.StatusCreated,
		Summary: "Make a draft expense claim from a read receipt",
		Description: "Saves the caller's own draft claim from a read receipt, from the claim as they corrected it (FR-DOC-12): the same draft, checks and audit as " +
			"createExpenseClaim, with the file attached to the claim as its evidence and the capture marked drafted. Nothing is submitted: the claimant submits it and " +
			"another person approves it (FR-AP-17). The claimant is always the caller. Side effects: the draft claim, its evidence, the capture's status and their audit " +
			"events. Needs If-Match with the capture's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing.",
	}, append(append([]string{CodeUnavailable, CodeClaimNotYours}, writes...), formCodes...), func(ctx context.Context, in *captureClaimInput) (*captureDraftedOutput, error) {
		return a.draftFromCapture(ctx, in.CaptureID, in.IfMatch, in.IdempotencyKey, in.Prefer, in.Body, storage.CaptureDraftClaim)
	})
	// The arrays of strings take their examples on their items.
	if s := a.OpenAPI().Components.Schemas.Map()["DocumentCapture"]; s != nil {
		if p := s.Properties["low_confidence"]; p != nil && p.Items != nil && len(p.Items.Examples) == 0 {
			p.Items.Examples = []any{"total"}
		}
	}
}

// safeName is a file name fit for a Content-Disposition header: no quotes, slashes, control characters or non-ASCII.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f && !strings.ContainsRune(`"\/;`, r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "document"
	}
	return b.String()
}

// adoptableFile is the attachment fileID of accepted mail messageID at the organisation's email-in address for documents, or at user's own, and
// the member whose own address it is (nil for the organisation's): any other combination is EML-10, and says no more.
func adoptableFile(ctx context.Context, u storage.UnitOfWork, user, messageID, fileID uuid.UUID) (storage.InboundFile, *uuid.UUID, error) {
	refused := problem(CodeMessageFile, "That is not an attachment of accepted mail at the organisation's email-in address or your own.",
		"List the mail, accept its sender if it waits, and choose one of its attachments.")
	m, err := u.InboundMessages().Get(ctx, messageID)
	if err != nil || m.Status != storage.InboundAccepted {
		return storage.InboundFile{}, nil, refused
	}
	addr, err := u.CaptureAddresses().Get(ctx, m.AddressID)
	if err != nil || addr.Kind != storage.CaptureDocuments || (addr.UserID != nil && *addr.UserID != user) {
		return storage.InboundFile{}, nil, refused
	}
	files, err := u.InboundMessages().Files(ctx, messageID)
	if err != nil {
		return storage.InboundFile{}, nil, err
	}
	for _, f := range files {
		if f.ID == fileID && f.Kind == storage.InboundAttachment && f.RemovedAt == nil {
			return f, addr.UserID, nil
		}
	}
	return storage.InboundFile{}, nil, refused
}

// captureFile keeps file in the staging store and adds it to the inbox, idempotently and previewably.
func (a *API) captureFile(ctx context.Context, p authn.Principal, file evidence.File, source, key, prefer string, message, inboundFile, owner *uuid.UUID) (*captureOutput, error) {
	blob := captureKey(p.Organisation)
	if !wantsPreview(prefer) {
		if err := a.deps.Staging.Put(ctx, blob, bytes.NewReader(file.Data)); err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
		}
	}
	request := struct{ SHA, Source, Message, File string }{file.SHA256, source, fmt.Sprint(message), fmt.Sprint(inboundFile)}
	w, err := write(ctx, a, key, prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DocumentCapture, error) {
		c, err := a.capturing(ctx, u, p, file, source, blob, message, inboundFile, owner)
		if err != nil {
			return DocumentCapture{}, err
		}
		return a.captureOf(c, p.UserID, nil), nil
	})
	if (err != nil || w.Replayed) && !wantsPreview(prefer) {
		_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), blob)
	}
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &captureOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &captureOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Location: "/v1/captures/" + w.Body.ID, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

// readingSchema is the strict output the model answers in: capdom.Reading's shape, every field a value (null when the document does not show it)
// and a confidence, every amount a decimal string (AI-27, ENG-10).
func readingSchema() *ports.ModelSchema {
	text := `{"type":"object","additionalProperties":false,"required":["value","confidence"],"properties":{"value":{"type":["string","null"],"maxLength":500},"confidence":{"type":"number","minimum":0,"maximum":1}}}`
	flag := `{"type":"object","additionalProperties":false,"required":["value","confidence"],"properties":{"value":{"type":["boolean","null"]},"confidence":{"type":"number","minimum":0,"maximum":1}}}`
	line := `{"type":"object","additionalProperties":false,"required":["description","net","gst_code","gst","amount"],"properties":{"description":` + text + `,"net":` + text + `,"gst_code":` + text + `,"gst":` + text + `,"amount":` + text + `}}`
	names := []string{"supplier", "supplier_uen", "gst_registration_no", "supplier_address", "number", "date", "due_date", "currency", "gst_total", "total", "gst_in_sgd"}
	props, req := []string{`"kind":{"type":"string","enum":["` + strings.Join(capdom.Kinds, `","`) + `"]}`}, []string{`"kind"`}
	for _, n := range names {
		props, req = append(props, `"`+n+`":`+text), append(req, `"`+n+`"`)
	}
	props = append(props, `"lines":{"type":"array","maxItems":100,"items":`+line+`}`, `"tax_invoice_label":`+flag, `"customer_named":`+flag)
	req = append(req, `"lines"`, `"tax_invoice_label"`, `"customer_named"`)
	return &ports.ModelSchema{Name: "reading", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":[` + strings.Join(req, ",") + `],"properties":{` + strings.Join(props, ",") + `}}`)}
}

// failCapture records why a reading did not end as read, in plain words, in a unit of work of its own, and returns the problem to say so.
func (a *API) failCapture(ctx context.Context, p authn.Principal, id uuid.UUID, why string, cause error) error {
	if err := a.deps.Store.Do(context.WithoutCancel(ctx), p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.DocumentCaptures().Lock(ctx, id)
		if err != nil || c.Status != storage.CaptureReading {
			return err
		}
		_, err = u.DocumentCaptures().Status(ctx, c.ID, c.Version, storage.CaptureFailed, why)
		return err
	}); err != nil {
		logFailure(ctx, err)
	}
	if cause != nil {
		return cause
	}
	return problem(CodeCaptureReading, why, "Type the document in by hand, or capture a clearer copy")
}

// readDocument is the reading of capture id as p (design D3): the file goes to the agent document_capture as an image or a PDF with no tools and a strict
// schema, its text screened first and the answer after; what comes back is parsed strictly, checked by the platform, and kept as the model's reading.
func (a *API) readDocument(ctx context.Context, p authn.Principal, id uuid.UUID, ifMatch string) (storage.DocumentCapture, error) {
	if a.deps.Staging == nil {
		return storage.DocumentCapture{}, problem(CodeUnavailable, "No file store is configured here.", "Ask your administrator to configure one (FR-DOC-01).")
	}
	// Everything that can be refused is checked before the status moves to Reading, so a refusal leaves the document as it was: the prompt, the
	// file's state and what it is.
	prompt, err := assistant.DefaultPrompt("document_capture")
	if err != nil {
		return storage.DocumentCapture{}, err
	}
	var c storage.DocumentCapture
	if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if c, err = visibleCapture(ctx, u, p, id, true); err != nil {
			return err
		}
		if err := checkIfMatch(ifMatch, docCaptureETag(c)); err != nil {
			return err
		}
		// A reading that never ended (a crash mid-call) is ended as Failed once it has been Reading for ReadingGrace, so no document is stranded.
		if c.Status == storage.CaptureReading && c.StatusAt != nil && a.deps.Clock.Now().Sub(*c.StatusAt) > ReadingGrace {
			if c, err = u.DocumentCaptures().Status(ctx, c.ID, c.Version, storage.CaptureFailed, "The reading did not finish: ask for it again."); err != nil {
				return err
			}
		}
		if !slices.Contains([]string{storage.CaptureReceived, storage.CaptureFailed, storage.CaptureRead}, c.Status) {
			return problem(CodeCaptureState, "A document that is "+c.Status+" is not read.", "Only a document waiting in the inbox is read; one being read is ended after "+ReadingGrace.String()+" if it never finishes")
		}
		if !mediaReadable(c.MediaType) {
			return problem(CodeCaptureType, "Only a PDF with text, or a text or email file, is read, and this is "+c.MediaType+": a photo cannot be checked for bank and identity numbers before the model sees it.",
				"Type the bill in by hand")
		}
		return nil
	}); err != nil {
		return storage.DocumentCapture{}, err
	}
	r, err := a.deps.Staging.Get(ctx, c.BlobKey)
	if err != nil {
		logFailure(ctx, err)
		return storage.DocumentCapture{}, problem(CodeUnavailable, "The file is no longer kept, so it could not be read.", "Capture it again")
	}
	data, err := io.ReadAll(io.LimitReader(r, evidence.MaxBytes+1))
	_ = r.Close()
	if err != nil {
		return storage.DocumentCapture{}, problem(CodeUnavailable, "The file could not be read in full.", "Try again")
	}
	masked, hidden, err := maskedTextOf(c.MediaType, data)
	if err != nil {
		return storage.DocumentCapture{}, err
	}
	// From here on every way out ends the reading in a status: Read, or Failed with why.
	if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.DocumentCaptures().Lock(ctx, id)
		if err != nil {
			return err
		}
		if cur.Version != c.Version {
			return problem(CodeStale, "The document changed while it was being prepared.", "Open it again")
		}
		c, err = u.DocumentCaptures().Status(ctx, cur.ID, cur.Version, storage.CaptureReading, "")
		return err
	}); err != nil {
		return storage.DocumentCapture{}, err
	}
	ask, _ := json.Marshal(map[string]any{"file_name": map[string]string{"untrusted_text": c.FileName}, "document_text": map[string]string{"untrusted_text": masked},
		"hidden_before_you_read": hidden, "task": "Read this document into the shape you were given."})
	screened := []string{c.FileName, masked}
	if err := a.screen(ctx, false, append(screened, string(ask))...); err != nil {
		return storage.DocumentCapture{}, a.failCapture(ctx, p, id, "Screening stopped the document before it reached the model: "+plainError(err), err)
	}
	res, err := a.router().Run(ctx, p.Organisation, ai.Call{Agent: "document_capture", Classes: []string{"business-figures", "personal-data"}, Actor: "user:" + p.UserID.String(),
		Request: ports.ModelRequest{System: prompt.Text, MaxTokens: 6000, Output: readingSchema(),
			Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(ask)}}}}}})
	if err != nil {
		pr := modelCallProblem(err)
		return storage.DocumentCapture{}, a.failCapture(ctx, p, id, pr.Detail, pr)
	}
	var text []string
	for _, x := range res.Response.Parts {
		if x.Kind == ports.PartText {
			text = append(text, x.Text)
		}
	}
	answer := strings.TrimSpace(strings.Join(text, ""))
	reading, perr := capdom.Parse([]byte(answer))
	if perr != nil {
		return storage.DocumentCapture{}, a.failCapture(ctx, p, id, "The model's answer was not a reading of the document: "+perr.Error(), nil)
	}
	if err := a.screen(ctx, true, answer); err != nil {
		return storage.DocumentCapture{}, a.failCapture(ctx, p, id, "Screening stopped the model's answer: "+plainError(err), err)
	}
	checked := capdom.Verify(reading, a.deps.Clock.Now())
	checks, _ := json.Marshal(checked)
	var out storage.DocumentCapture
	if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.DocumentCaptures().Lock(ctx, id)
		if err != nil {
			return err
		}
		out, err = u.DocumentCaptures().Reading(ctx, id, cur.Version, storage.CapturedReading{Kind: reading.Kind, Reading: json.RawMessage(answer), Model: res.Response.Model,
			Prompt: prompt.Name(), Checks: checks, At: a.deps.Clock.Now()})
		return err
	}); err != nil {
		logFailure(ctx, err)
		return storage.DocumentCapture{}, a.failCapture(ctx, p, id, "The reading could not be kept: ask for it again.", nil)
	}
	return out, nil
}

// saveSupplierDefaults keeps the confirmed bill's first line's account and GST code as its supplier's defaults, where the contact has none (FR-AP-12):
// as the person, who must hold the right to change contacts. A default the contact already holds is never replaced.
func saveSupplierDefaults(ctx context.Context, u storage.UnitOfWork, p authn.Principal, d bill.Draft) error {
	if !authz.Decide(authz.UpdateContact, p.Roles, false).Allowed {
		return problem(CodeForbidden, "Saving a supplier's defaults changes its contact, which your roles may not do.", "Make the draft without saving the defaults, or ask someone who may change contacts")
	}
	if len(d.Lines) == 0 {
		return nil
	}
	c, err := u.Contacts().Get(ctx, d.SupplierID)
	if err != nil {
		return err
	}
	details, changed := c.ContactDetails, false
	if details.DefaultAccountID == nil {
		id := d.Lines[0].AccountID
		details.DefaultAccountID, changed = &id, true
	}
	if details.DefaultGSTCode == "" && d.Lines[0].GSTCode != "" {
		details.DefaultGSTCode, changed = d.Lines[0].GSTCode, true
	}
	if !changed {
		return nil
	}
	_, err = u.Contacts().Update(ctx, c.ID, c.Version, details, c.Active)
	return err
}

// draftFromCapture makes the draft a person asked for from a read capture, as the person, and attaches the file to it as evidence (design D4).
func (a *API) draftFromCapture(ctx context.Context, captureID, ifMatch, key, prefer string, body any, kind string) (*captureDraftedOutput, error) {
	id, err := parseID("capture_id", captureID)
	if err != nil {
		return nil, err
	}
	p, _ := authn.From(ctx)
	if kind == storage.CaptureDraftClaim {
		if p, err = claimant(ctx); err != nil {
			return nil, err
		}
	}
	if a.deps.Staging == nil {
		return nil, problem(CodeUnavailable, "No file store is configured here.", "Ask your administrator to configure one (FR-DOC-01).")
	}
	w, err := write(ctx, a, key, prefer, struct {
		Capture string
		Body    any
	}{captureID, body}, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CaptureDrafted, error) {
		c, err := visibleCapture(ctx, u, p, id, true)
		if err != nil {
			return CaptureDrafted{}, err
		}
		if err := checkIfMatch(ifMatch, docCaptureETag(c)); err != nil {
			return CaptureDrafted{}, err
		}
		if c.Status != storage.CaptureRead {
			return CaptureDrafted{}, problem(CodeCaptureState, "A document that is "+c.Status+" cannot be drafted.", "Read it first; a drafted or dismissed document is final")
		}
		org, err := u.Organisation(ctx)
		if err != nil {
			return CaptureDrafted{}, err
		}
		base, err := money.Lookup(org.BaseCurrency)
		if err != nil {
			return CaptureDrafted{}, err
		}
		var doc storage.Document
		switch b := body.(type) {
		case CaptureBillDraft:
			d, err := billDraftOf(b.BillDraft, base)
			if err != nil {
				return CaptureDrafted{}, err
			}
			if err := a.checkBillableLines(ctx, u, d.Lines); err != nil {
				return CaptureDrafted{}, err
			}
			if doc, err = journal.CreateBillIn(ctx, u, d); err != nil {
				return CaptureDrafted{}, err
			}
			checked, err := journal.CheckBill(ctx, u, doc)
			if err != nil {
				return CaptureDrafted{}, err
			}
			if b.SaveDefaults {
				if err := saveSupplierDefaults(ctx, u, p, d); err != nil {
					return CaptureDrafted{}, err
				}
			}
			for _, w := range checked.Prepared.Warnings {
				if w.Code != bill.WarnDuplicate {
					continue
				}
				reason := strings.TrimSpace(b.DuplicateReason)
				if reason == "" {
					return CaptureDrafted{}, problem(CodeCaptureDuplicate, w.Message+": it is refused as a duplicate.", "Open the other bill, or say why this one is entered in duplicate_reason (FR-AP-12)")
				}
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "document_capture.duplicate_accepted", ObjectType: "document_capture", ObjectID: c.ID,
					Changes: map[string][2]any{"reason": {nil, reason}, "message": {nil, w.Message}}}); err != nil {
					return CaptureDrafted{}, err
				}
			}
		case ExpenseClaimDraft:
			v, err := a.claimView(ctx, u)
			if err != nil {
				return CaptureDrafted{}, err
			}
			d, err := claimDraftOf(b, v.base)
			if err != nil {
				return CaptureDrafted{}, err
			}
			if err := a.checkBillableClaimLines(ctx, u, d.Lines); err != nil {
				return CaptureDrafted{}, err
			}
			d.ClaimantID = p.UserID
			if doc, err = journal.CreateClaimIn(ctx, u, d); err != nil {
				return CaptureDrafted{}, err
			}
		}
		// The file becomes the draft's evidence: a copy in the evidence staging area, committed as the draft's own are (FR-DOC-13).
		r, err := a.deps.Staging.Get(ctx, c.BlobKey)
		if err != nil {
			logFailure(ctx, err)
			return CaptureDrafted{}, problem(CodeUnavailable, "The captured file is no longer kept.", "Capture it again.")
		}
		data, err := io.ReadAll(io.LimitReader(r, evidence.MaxBytes+1))
		_ = r.Close()
		if err != nil {
			return CaptureDrafted{}, err
		}
		evKey := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
		if !storage.IsPreview(ctx) {
			if err := a.deps.Staging.Put(ctx, evKey, bytes.NewReader(data)); err != nil {
				logFailure(ctx, err)
				return CaptureDrafted{}, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
			}
		}
		source := map[string]storage.EvidenceSource{storage.CaptureUpload: storage.SourceUpload, storage.CapturePaste: storage.SourcePaste, storage.CaptureEmail: storage.SourceEmail}[c.Source]
		evType := "invoice"
		if kind == storage.CaptureDraftClaim {
			evType = "receipt"
		}
		if _, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: evType, Note: "Captured in the document inbox", FileName: c.FileName, MediaType: c.MediaType,
			Size: int64(len(data)), SHA256: c.SHA256, OriginalSHA256: c.SHA256, NRICFlagged: c.NRICFlagged, Source: source, DataClass: storage.ClassGeneral, StagingKey: evKey, UploadedBy: uploader(ctx)}); err != nil {
			return CaptureDrafted{}, err
		}
		done, err := u.DocumentCaptures().Drafted(ctx, c.ID, c.Version, kind, doc.ID, p.UserID, a.deps.Clock.Now())
		if err != nil {
			return CaptureDrafted{}, err
		}
		return CaptureDrafted{Capture: a.captureOf(done, p.UserID, nil), Draft: CaptureDraft{Kind: kind, ID: doc.ID.String()}}, nil
	})
	if err != nil {
		return nil, err
	}
	return &captureDraftedOutput{Status: w.Status, ETag: etag(w.Body.Capture.Version, w.Body.Capture.Status), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}

var _ = claim.CodeNotClaim
