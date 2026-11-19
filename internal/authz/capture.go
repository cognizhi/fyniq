package authz

// The document inbox's operations (PRD-001 FR-DOC-12, FR-DOC-14; p1-document-capture): a file captured for a bill or a claim, read by the
// model, and drafted by a person. Who captures, reads and drafts is who may enter bills (the AP specialist, the Bookkeeper, the Accountant and
// the roles above them); a draft claim is made by someone who may claim, as their own. The AI holds none of these: a reading is made by the
// API's own call of the model, and a draft is a person's act.
var (
	CaptureDocument = Policy{Operation: "captureDocument", Capability: Bills, Need: Create,
		Note: "Adds a file to the document inbox. The AI and MCP clients capture nothing: a person uploads or pastes it, or adopts a file of accepted mail."}
	CaptureFromMessage = Policy{Operation: "captureFromMessage", Capability: Bills, Need: Create,
		Note: "Adopts a file of accepted mail at the organisation's, or the caller's own, email-in address into the document inbox."}
	ListCaptures   = Policy{Operation: "listCaptures", Capability: Bills, Need: View}
	GetCapture     = Policy{Operation: "getCapture", Capability: Bills, Need: View}
	GetCaptureFile = Policy{Operation: "getCaptureFile", Capability: Bills, Need: View,
		Note: "Downloads the captured file as an attachment, never shown inline: it is untrusted (AI-18)."}
	ReadCapture = Policy{Operation: "readCapture", Capability: Bills, Need: Create,
		Note: "Asks the model to read the document, as the caller, within the AI switch and the budgets: a read, with no write (AI-18, AI-28)."}
	DismissCapture       = Policy{Operation: "dismissCapture", Capability: Bills, Need: Create}
	DraftBillFromCapture = Policy{Operation: "draftBillFromCapture", Capability: Bills, Need: Create,
		Note: "Makes a draft bill from a read document, as the caller, from the reading as they corrected it. The AI and MCP clients only propose a draft (ap.draft_bill)."}
	DraftClaimFromCapture = Policy{Operation: "draftClaimFromCapture", Capability: ExpenseClaims, Need: Create, Parent: Bills,
		Note: "Makes the caller's own draft claim from a read receipt."}
)

// The rest of the AI proposals (PRD-001 AI-31, AI-91, AI-92, CTL-AI-07; p1-ai-proposals): undoing an executed proposal, and the month's approval-quality
// report and its signed review. Each is a person's, on the AI activity log's row, decided by the record or the role in the handler: the AI's column
// is "—", so no AI or MCP client reaches them (§9.6).
var (
	UndoAIProposal = Policy{Operation: "undoAIProposal", Capability: AIActivity, Need: View, ByRecord: true,
		Note: "Undoes an executed proposal as the person who holds the right its command needs, through the domain's own reversal (AI-31, AI-91)."}
	GetAIApprovalQuality = Policy{Operation: "getAIApprovalQuality", Capability: AIActivity, Need: View, ByRecord: true,
		Note: "The month's approval-quality metrics, for the Owner and the DPO (CTL-AI-07)."}
	SignAIApprovalReview = Policy{Operation: "signAIApprovalReview", Capability: AIActivity, Need: View, ByRecord: true,
		Note: "Signs the month's review of the approval-quality metrics, as the Owner or the DPO, after step-up (CTL-AI-07)."}
)
