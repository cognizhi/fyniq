package assistant

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Questions (NM-AI-02, 03, 06). The runtime asks a model which read tools to call, runs them as the asking person, and asks the model to write the answer from what they returned. Everything a person
// typed is data, never an instruction: it is wrapped as untrusted, a tool the model names that is not in the allowlist or that the never-list refuses is dropped, and every figure of the answer must be
// one a tool returned in the same turn. No pay figure and no sealed field reaches a model: a tool that returns one is refused before the model sees it.

// Tool names: what the assistant may read, each as the asking person.
const (
	ToolLeaveBalance = "my_leave_balance"
	ToolPayDay       = "my_payday"
	ToolPolicy       = "rule_answer"
	ToolPayslipLine  = "slip_line_rule"
)

// Tools is the allowlist, each with what it is for. A tool is read-only and takes at most one argument.
var Tools = map[string]string{
	ToolLeaveBalance: "The asking person's own annual leave balance for the year: entitled, taken, booked and left, in days.",
	ToolPayDay:       "The next day the asking person is paid, from their pay group's calendar. No amount.",
	ToolPolicy:       "What the Singapore people pack says about a rule (annual leave entitlement, public holidays, notice), with its source and whether it is confirmed.",
	ToolPayslipLine:  "What a line of a payslip is and how it is worked out (basic salary, overtime, CPF, a deduction), as a rule. No figure: the page shows the person's own figures beside it.",
}

// Call is a tool the model asked for.
type Call struct {
	Tool string
	Arg  string
}

// Result is what a tool returned: facts as text, where they come from, and whether the source is confirmed. Sensitive results are never sent to a model.
type Result struct {
	Tool    string
	Facts   map[string]string
	Sources []string
	Confirm bool // the value is researched but not yet verified: the answer says to confirm with HR
}

// Segment is a sentence of an answer with the sources it rests on.
type Segment struct {
	Text    string
	Sources []string
}

// Answer is what the person sees.
type Answer struct {
	Segments []Segment
	Tools    []string // the tools that ran
	Dropped  []string // tools the model named that were refused, with why
	Refused  string   // set when the question asks for something the assistant never does
}

// Model is the route: it plans which tools to call and composes the answer from their results. The scripted model of local mode implements it; a real route needs processing evidenced in Singapore.
type Model interface {
	ID() string
	Plan(ctx context.Context, question string) ([]Call, error)
	Compose(ctx context.Context, question string, results []Result) ([]Segment, error)
}

// Runner runs a tool as the asking person: through the same row-level security and role checks as the web app.
type Runner interface {
	Run(ctx context.Context, c Call) (Result, error)
}

// MaxQuestion is the longest question: longer is refused in words.
const MaxQuestion = 1000

// money matches a dollar sign before a figure, a figure with a thousands comma, and a figure of four digits or more with cents: a leave balance in days (14.00) is none of them.
var money = regexp.MustCompile(`(?i)(S?\$\s?\d|\d{1,3},\d{3}|\d{4,}\.\d{2}\b)`)

// payKeys are the fact names that carry pay: a result with one never reaches a model.
var payKeys = []string{"basic", "gross", "net", "salary", "amount", "cpf", "sdl", "bonus", "allowance", "deduction", "overtime_pay", "bank", "nric", "account"}

// Sensitive reports whether a tool result holds pay, a sealed field or a figure of money, which no model may see (NM-AI-06, AI-02).
func Sensitive(r Result) bool {
	for k, v := range r.Facts {
		lk := strings.ToLower(k)
		for _, p := range payKeys {
			if strings.Contains(lk, p) {
				return true
			}
		}
		if money.MatchString(v) {
			return true
		}
	}
	return false
}

// askNever names what a question asks for that the assistant never does, in words, or "" when it asks for nothing of the kind: a request to approve, change, issue, pay, read another person's data or
// reveal a sealed field is answered with a refusal and no tool runs.
var askNever = []struct {
	pattern *regexp.Regexp
	what    string
}{
	{regexp.MustCompile(`(?i)\b(approve|reject|refuse|decline)\b.*\b(leave|request|claim|letter|run|payslip)\b`), "approving or refusing anything"},
	{regexp.MustCompile(`(?i)\b(change|update|set|raise|increase|cut|edit)\b.*\b(salary|pay|bank|account|role|record|permission)\b`), "changing pay, a bank account, a role or a record"},
	{regexp.MustCompile(`(?i)\b(issue|sign)\b.*\b(letter|document|kets)\b`), "issuing or signing a letter"},
	{regexp.MustCompile(`(?i)\b(nric|fin number|bank account number|account number|passport|medical|mc\b)`), "reading restricted data such as an NRIC, a bank account or a medical certificate"},
	{regexp.MustCompile(`(?i)\b(everyone|everybody|all (the )?(staff|employees|people)|my team|his|her|their|colleague|someone else|another person)\b.*\b(salary|pay|payslip|leave balance|bonus|bank)\b`), "reading another person's pay or records"},
	{regexp.MustCompile(`(?i)\b(salary|pay|payslip|bonus|leave balance)\b.*\b(of|for)\b\s+[A-Z][a-z]+`), "reading another person's pay or records"},
	{regexp.MustCompile(`(?i)ignore (all |the |your )?(previous|above|prior) (instructions|rules)|disregard (your )?rules|you are now|system prompt`), "following an instruction hidden in a question"},
}

// Ask answers a question: it refuses what the assistant never does, asks the model which tools to call, drops any that are not on the allowlist or that the never-list refuses, runs the rest as the asking
// person, refuses a result that holds pay or a sealed field before a model sees it, asks the model to compose, and checks that every figure of the answer came from a tool in this turn.
func Ask(ctx context.Context, m Model, r Runner, question string) (Answer, error) {
	var a Answer
	q := strings.TrimSpace(question)
	switch {
	case q == "":
		return a, invalid("question: write what you want to know.")
	case len([]rune(q)) > MaxQuestion:
		return a, invalid("question: keep it to %d characters.", MaxQuestion)
	}
	for _, n := range askNever {
		if n.pattern.MatchString(q) {
			a.Refused = "The assistant never does anything that means " + n.what + ". Ask the people team, or ask me what a rule says."
			a.Segments = []Segment{{Text: a.Refused}}
			return a, nil
		}
	}
	calls, err := m.Plan(ctx, q)
	if err != nil {
		return a, err
	}
	var results []Result
	for _, c := range calls {
		if _, ok := Tools[c.Tool]; !ok || Refuse(c.Tool) != "" {
			a.Dropped = append(a.Dropped, c.Tool+": not a tool the assistant may use")
			continue
		}
		res, err := r.Run(ctx, c)
		if err != nil {
			return a, err
		}
		if Sensitive(res) {
			a.Dropped = append(a.Dropped, c.Tool+": its result holds pay or a sealed field and is not sent to a model")
			continue
		}
		a.Tools = append(a.Tools, c.Tool)
		results = append(results, res)
	}
	segs, err := m.Compose(ctx, q, results)
	if err != nil {
		return a, err
	}
	a.Segments = Ground(segs, results)
	return a, nil
}

var number = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// Ground keeps an answer's figures honest (AI-08): a number in a sentence that no tool returned in this turn is replaced with "an unverified figure", and a sentence with no source gets the
// sources of the turn's results.
func Ground(segs []Segment, results []Result) []Segment {
	seen := map[string]bool{}
	var sources []string
	for _, r := range results {
		for _, v := range r.Facts {
			for _, n := range number.FindAllString(v, -1) {
				seen[strings.ReplaceAll(n, ",", "")] = true
			}
		}
		for _, s := range r.Sources {
			if !slices.Contains(sources, s) {
				sources = append(sources, s)
			}
		}
	}
	var out []Segment
	for _, s := range segs {
		s.Text = number.ReplaceAllStringFunc(s.Text, func(n string) string {
			if seen[strings.ReplaceAll(n, ",", "")] {
				return n
			}
			return "an unverified figure"
		})
		if len(s.Sources) == 0 {
			s.Sources = sources
		}
		out = append(out, s)
	}
	return out
}

// ConfirmNote is added to an answer whose source is researched but not yet verified (NM-AI-03).
const ConfirmNote = "This rule is read from an official page but has not been verified a second time: confirm it with HR before you rely on it."

// Scripted is local mode's model: it picks tools from the words of the question and writes the answer from templates, so every evaluation runs offline and the same question always gets the same answer.
// It is not a language model and it is never part of a release build.
type Scripted struct{}

func (Scripted) ID() string { return "scripted" }

var payslipTopics = []string{"cpf", "overtime", "basic", "deduction", "allowance", "bonus", "sdl", "net pay", "gross"}

func (Scripted) Plan(_ context.Context, q string) ([]Call, error) {
	l := strings.ToLower(q)
	var out []Call
	has := func(ws ...string) bool {
		for _, w := range ws {
			if strings.Contains(l, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("payslip", "pay slip") && has(payslipTopics...):
		for _, t := range payslipTopics {
			if strings.Contains(l, t) {
				out = append(out, Call{Tool: ToolPayslipLine, Arg: t})
				break
			}
		}
	case has("pay day", "payday", "when am i paid", "when do i get paid"):
		out = append(out, Call{Tool: ToolPayDay})
	case has("how many days", "entitle", "public holiday", "notice", "sick", "maternity", "paternity", "childcare") && !has("my balance", "do i have", "left"):
		out = append(out, Call{Tool: ToolPolicy, Arg: policyTopic(l)})
	case has("leave balance", "how much leave", "days left", "leave do i have", "my leave", "annual leave"):
		out = append(out, Call{Tool: ToolLeaveBalance})
	}
	return out, nil
}

func policyTopic(l string) string {
	switch {
	case strings.Contains(l, "public holiday"):
		return "public_holiday"
	case strings.Contains(l, "sick"):
		return "sick_leave"
	default:
		return "annual_leave"
	}
}

func (Scripted) Compose(_ context.Context, q string, results []Result) ([]Segment, error) {
	if len(results) == 0 {
		return []Segment{{Text: "I can answer questions about your own leave balance, your pay day, what a rule of the Singapore pack says and what a line of your payslip is. I could not match your question to one of those: try asking it another way, or ask the people team."}}, nil
	}
	var out []Segment
	for _, r := range results {
		switch r.Tool {
		case ToolLeaveBalance:
			out = append(out, Segment{Text: fmt.Sprintf("For %s you are entitled to %s days of annual leave, have taken %s, have %s booked and have %s left.", r.Facts["year"], r.Facts["entitled"], r.Facts["taken"], r.Facts["booked"], r.Facts["left"]), Sources: r.Sources})
		case ToolPayDay:
			out = append(out, Segment{Text: fmt.Sprintf("Your next pay day is %s, from the calendar of your pay group.", r.Facts["next_pay_day"]), Sources: r.Sources})
		case ToolPolicy:
			out = append(out, Segment{Text: r.Facts["answer"], Sources: r.Sources})
		case ToolPayslipLine:
			out = append(out, Segment{Text: r.Facts["rule"] + " Your own figures for this line are on your payslip page, beside it.", Sources: r.Sources})
		}
		if r.Confirm {
			out = append(out, Segment{Text: ConfirmNote, Sources: r.Sources})
		}
	}
	return out, nil
}
