package proposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The company secretary's task types (PRD-001 Appendix E, AI-65; p1-scheduled-agents tasks 6.1 and 6.2).

// CreateComplianceTaskPolicy is what a person needs to raise a compliance task themselves: C on the AI company
// secretary's row, which the Administrator holds; the Owner, whose cell is A, raises one too, as they close one. The
// Company secretary approves one under Appendix D's rule for every sec.* proposal (Approves).
var CreateComplianceTaskPolicy = authz.Policy{Operation: "createComplianceTask", Capability: authz.AICompanySecretary, Need: authz.Create, OwnerMay: true,
	Note: "Raised by an approved sec.create_task proposal (AI-65); the Owner holds A on the row, not C, and raises one as the Administrator does."}

// CommandPolicy is the policy of the command operation op names: the API's own operation's (authz.Policies), else the
// release's task types' command's whose operation it is. Nil Operation: none, and no one signs.
func CommandPolicy(op string) authz.Policy {
	if p, ok := authz.Policies[op]; ok {
		return p
	}
	for _, c := range Release {
		if p := c.Policy(); p.Operation == op {
			return p
		}
	}
	return authz.Policy{}
}

// Approves reports whether roles hold the right to approve a proposal of taskType whose command needs p: p itself, as
// if they did it themselves (rev 0.280), or, for a sec.* task type, the Company secretary's A on every sec.* proposal,
// its tasks included (Appendix D, rev 0.6 and rev 0.280).
func Approves(taskType string, p authz.Policy, roles []authz.Role) bool {
	if p.Operation == "" {
		return false
	}
	if authz.Decide(p, roles, false).Allowed {
		return true
	}
	return strings.HasPrefix(taskType, "sec.") && slices.Contains(roles, authz.CompanySecretary)
}

// taskRoles are the roles a task may be owned by: the company secretary assistant's recipients' (its setting's choices).
var taskRoles = []string{"owner", "administrator", "controller", "accountant", "company_secretary", "board_viewer"}

// newTask is sec.create_task's payload.
type newTask struct {
	FindingKey string `json:"finding_key"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	OwnerRole  string `json:"owner_role"`
	DueOn      string `json:"due_on"`
}

func (t newTask) due() (time.Time, error) {
	d, err := time.Parse(time.DateOnly, t.DueOn)
	if err != nil {
		return time.Time{}, &storage.Error{Code: storage.CodeInvalid, Message: "payload.due_on is not a date, such as 2027-01-31", Hint: "Give the date it is due by"}
	}
	return d, nil
}

func (t newTask) base() string { return "compliance_task:" + t.FindingKey }

// CreateComplianceTask is sec.create_task's command (Appendix E; design D7): a compliance task the model suggests, with an
// owner and a due date, for a finding of its own (agent:…). A finding with a task open, or one proposed and still
// waiting, gets none: the task type is deduplicated per finding. Deterministic findings raise their tasks directly, never
// through it (AI-65).
type CreateComplianceTask struct{}

// Policy implements Command.
func (CreateComplianceTask) Policy() authz.Policy { return CreateComplianceTaskPolicy }

// Measure implements Command: one task, no amount.
func (CreateComplianceTask) Measure(payload json.RawMessage) (Measure, error) {
	var t newTask
	if err := json.Unmarshal(payload, &t); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command: what it would create, refused for a finding that has a task open or proposed.
func (CreateComplianceTask) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var t newTask
	if err := json.Unmarshal(payload, &t); err != nil {
		return Impact{}, err
	}
	due, err := t.due()
	if err != nil {
		return Impact{}, err
	}
	if !strings.HasPrefix(t.FindingKey, "agent:") || !slices.Contains(taskRoles, t.OwnerRole) {
		return Impact{}, &storage.Error{Code: storage.CodeInvalid, Message: "payload.finding_key is not one of the agent's own (agent:…), or payload.owner_role is not a recipient's role",
			Hint: "Name a finding of your own cross-check, and one of the roles the assistant tells"}
	}
	if open, err := u.ComplianceTasks().OpenFor(ctx, t.FindingKey); err == nil {
		return Impact{}, &storage.Error{Code: storage.CodeDuplicate, Message: fmt.Sprintf("the finding %s already has an open task, %q", t.FindingKey, open.Title),
			Hint: "Propose no second task for it"}
	} else if !errors.Is(err, ports.ErrNotFound) {
		return Impact{}, err
	}
	waiting, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Statuses: []string{storage.ProposalPending, storage.ProposalApproved}, Limit: 100})
	if err != nil {
		return Impact{}, err
	}
	for _, p := range waiting {
		var other newTask
		if p.TaskType == "sec.create_task" && json.Unmarshal([]byte(p.Payload), &other) == nil && other.FindingKey == t.FindingKey {
			return Impact{}, &storage.Error{Code: storage.CodeDuplicate, Message: fmt.Sprintf("a task for the finding %s is proposed already and waits for a person", t.FindingKey),
				Hint: "Propose no second task for it"}
		}
	}
	changes, err := json.Marshal(map[string]any{"compliance_tasks": []map[string]any{{"change": "create", "finding_key": t.FindingKey, "title": t.Title,
		"detail": t.Detail, "owner_role": t.OwnerRole, "due_on": t.DueOn}}})
	if err != nil {
		return Impact{}, err
	}
	role := strings.ReplaceAll(t.OwnerRole, "_", " ")
	return Impact{Summary: fmt.Sprintf("Creates the task %q for the %s, due %s", t.Title, role, due.Format("2 January 2006")), Changes: changes,
		Base: map[string]int64{t.base(): 0}}, nil
}

// Execute implements Command: the task, raised for the proposal as the approving person, unless one opened meanwhile.
func (CreateComplianceTask) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var t newTask
	if err := json.Unmarshal(payload, &t); err != nil {
		return Execution{}, err
	}
	due, err := t.due()
	if err != nil {
		return Execution{}, err
	}
	if _, ok := base[t.base()]; !ok {
		return Execution{}, fmt.Errorf("the dry run did not read %s", t.base())
	}
	if _, err := u.ComplianceTasks().OpenFor(ctx, t.FindingKey); err == nil {
		return Execution{}, ErrStale // a task opened for the finding since the dry run
	} else if !errors.Is(err, ports.ErrNotFound) {
		return Execution{}, err
	}
	made, err := u.ComplianceTasks().RaiseForProposal(ctx, as.Proposal, storage.NewComplianceTask{FindingKey: t.FindingKey, Title: t.Title,
		Detail: t.Detail, OwnerRole: t.OwnerRole, DueOn: &due, CreatedAt: as.Today})
	if storage.CodeOf(err) == storage.CodeDuplicate { // the open task of its finding, raised meanwhile
		return Execution{}, ErrStale
	}
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{"compliance_task:" + made.ID.String()}, Entries: []string{}}, nil
}

// reminder is sec.draft_reminder's payload: the role its members hold, its subject and message, and the obligation it
// concerns, by its finding's or calendar item's key.
type reminder struct {
	ToRole     string `json:"to_role"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
	Obligation string `json:"obligation"`
}

// reminderNote ends every reminder, so its readers know who wrote it and that a person approved it.
const reminderNote = "This reminder was drafted by Fyniq's AI assistant and approved by a person in Fyniq before it was sent."

// text is the email as it is sent.
func (r reminder) text() string {
	return strings.TrimSpace(r.Message) + "\n\nAbout: " + r.Obligation + "\n\n" + reminderNote + "\n"
}

// recipients are the sign-in emails of the members holding the reminder's role now.
func (r reminder) recipients(ctx context.Context, u storage.UnitOfWork, now time.Time) ([]string, error) {
	emails, err := u.InboundMessages().MemberEmails(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]uuid.UUID, 0, len(emails))
	for user := range emails {
		users = append(users, user)
	}
	slices.SortFunc(users, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	var out []string
	for _, user := range users {
		roles, err := u.Members().Roles(ctx, user, now)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return nil, err
		}
		if slices.ContainsFunc(roles, func(a storage.RoleAssignment) bool { return a.Role == r.ToRole }) {
			out = append(out, emails[user])
		}
	}
	return out, nil
}

// DraftReminder is sec.draft_reminder's command (Appendix E; design D8): a reminder the model drafted about an
// obligation, to the members holding a role, sent only once a person approves it. The dry run renders the email and
// sends nothing; executed, it is queued through the email outbox as the approving person, once.
type DraftReminder struct{}

// Policy implements Command: the right to raise the obligation's task, as a reminder is one to a person (C on the AI
// company secretary's row; the Company secretary approves under Approves).
func (DraftReminder) Policy() authz.Policy { return CreateComplianceTaskPolicy }

// Measure implements Command: one reminder, no amount.
func (DraftReminder) Measure(payload json.RawMessage) (Measure, error) {
	var r reminder
	if err := json.Unmarshal(payload, &r); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command: the email as it would be sent, and to how many members; refused for a role no member with
// a sign-in email holds.
func (DraftReminder) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var r reminder
	if err := json.Unmarshal(payload, &r); err != nil {
		return Impact{}, err
	}
	if !slices.Contains(taskRoles, r.ToRole) {
		return Impact{}, &storage.Error{Code: storage.CodeInvalid, Message: "payload.to_role is not a role the assistant tells", Hint: "Name one of the recipients' roles"}
	}
	to, err := r.recipients(ctx, u, as.Today)
	if err != nil {
		return Impact{}, err
	}
	if len(to) == 0 {
		return Impact{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("no member holding the role %s has a sign-in email", r.ToRole),
			Hint: "Remind a role whose members can be emailed"}
	}
	changes, err := json.Marshal(map[string]any{"emails": []map[string]any{{"change": "send", "to_role": r.ToRole, "recipients": len(to),
		"subject": r.Subject, "text": r.text()}}})
	if err != nil {
		return Impact{}, err
	}
	role := strings.ReplaceAll(r.ToRole, "_", " ")
	people := "1 person"
	if len(to) != 1 {
		people = fmt.Sprintf("%d people", len(to))
	}
	return Impact{Summary: fmt.Sprintf("Emails the reminder %q to the %s (%s)", r.Subject, role, people), Changes: changes, Base: map[string]int64{}}, nil
}

// Execute implements Command: the reminder, queued to each member holding the role now, through the executor's outbox.
func (DraftReminder) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, _ map[string]int64, as Actor) (Execution, error) {
	var r reminder
	if err := json.Unmarshal(payload, &r); err != nil {
		return Execution{}, err
	}
	to, err := r.recipients(ctx, u, as.Today)
	if err != nil {
		return Execution{}, err
	}
	if len(to) == 0 {
		return Execution{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("no member holding the role %s has a sign-in email now", r.ToRole)}
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return Execution{}, err
	}
	out := Execution{Records: []string{}, Entries: []string{}}
	for _, addr := range to {
		m, err := email.Queue(ctx, as.Outbox, u, o.ID, email.PurposeSecretaryReminder, addr, r.Subject, r.text())
		if err != nil {
			return Execution{}, err
		}
		out.Records = append(out.Records, "email_message:"+m.ID.String())
	}
	return out, nil
}
