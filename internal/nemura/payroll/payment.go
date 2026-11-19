package payroll

// The states of a person's payment in a pay run (PRD-002 NM-PAY-10). The database enforces the same moves (migration 000031); this is the one list the API, the link's inbox and the screens share.
const (
	PayScheduled = "scheduled"
	PayReleased  = "released"
	PaySent      = "sent"
	PayAccepted  = "accepted"
	PayPaid      = "paid"
	PayRejected  = "rejected"
	PayReturned  = "returned"
	PayUnknown   = "unknown"
)

// PaymentStates lists the states in the order a payment normally moves through them.
var PaymentStates = []string{PayScheduled, PayReleased, PaySent, PayAccepted, PayPaid, PayRejected, PayReturned, PayUnknown}

var moves = map[string][]string{
	PayScheduled: {PayReleased, PaySent, PayAccepted, PayPaid, PayRejected, PayUnknown},
	PayReleased:  {PaySent, PayAccepted, PayPaid, PayRejected, PayUnknown},
	PaySent:      {PayAccepted, PayPaid, PayRejected, PayReturned, PayUnknown},
	PayAccepted:  {PayPaid, PayRejected, PayReturned, PayUnknown},
	PayPaid:      {PayReturned},
	PayRejected:  {PayScheduled},
	PayReturned:  {PayScheduled},
	PayUnknown:   {PayPaid, PayRejected, PayReturned},
}

// ValidState says whether s is one of the eight states.
func ValidState(s string) bool {
	_, ok := moves[s]
	return ok
}

// LegalMove says whether a payment in state from may move to state to. The first event of a payment may be any state. An unknown outcome is resolved as paid, rejected or returned before
// a new payment (a new "scheduled") can start, so it blocks the person's next payment in that run.
func LegalMove(from, to string) bool {
	if from == "" {
		return ValidState(to)
	}
	for _, t := range moves[from] {
		if t == to {
			return true
		}
	}
	return false
}

// NeedsAttention says whether a payment's latest state is a problem for the people who run payroll: rejected, returned or unknown (N.1 #14).
func NeedsAttention(state string) bool {
	return state == PayRejected || state == PayReturned || state == PayUnknown
}

// RunPayState is the run's payment state from its people's: "approved" while nobody is paid, "paid in part" while some are, "paid" when every person owed money is. A person with nothing to pay
// (a disbursement of zero) is not counted.
func RunPayState(owed int, paid int) string {
	switch {
	case owed > 0 && paid >= owed:
		return "paid"
	case paid > 0:
		return "paid in part"
	default:
		return "approved"
	}
}
