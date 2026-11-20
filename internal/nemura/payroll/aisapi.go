package payroll

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The submission of a payload to IRAS (PRD-002 NM-PAY-14; p2-payroll-outputs design D3). IRAS's AIS-API 2.0 is a REST call to
// sandbox.api.gov.sg or public.api.gov.sg with a Corppass access token (scope EmpIncomeSub) and an APEX API key in the headers, from a server on an allow-listed
// address (section 3.1 to 3.5). None of that exists until IRAS onboards the software and the employer consents: the real adapter is [live-verify]. The port, and a stub
// that answers as the specification's sample responses do (section 3.8), are what is built.

// AISReturn is IRAS's returnCode (section 3.8): 10 success, 20 success with warnings, 30 error (not processed), 40 warning (not processed).
const (
	AISSuccess     = "10"
	AISWarnings    = "20"
	AISError       = "30"
	AISWarningStop = "40"
)

// AISFieldInfo is one entry of IRAS's fieldInfoList or warnings: where the problem is and what to correct. RecordID is the records it is on, "1, 2", or "NA".
type AISFieldInfo struct {
	Section  string `json:"section"`
	Field    string `json:"field"`
	Message  string `json:"message"`
	RecordID string `json:"recordId"`
}

// AISResponse is IRAS's answer: the acknowledgement code of a successful submission, or what to correct.
type AISResponse struct {
	ReturnCode string `json:"returnCode"`
	Data       struct {
		AcknowledgementCode string `json:"acknowledgementCode,omitempty"`
	} `json:"data"`
	Info struct {
		MessageCode string         `json:"messageCode,omitempty"`
		Message     string         `json:"message,omitempty"`
		Warnings    []AISFieldInfo `json:"warnings"`
		FieldInfo   []AISFieldInfo `json:"fieldInfoList"`
	} `json:"info"`
}

// Accepted says whether IRAS took the submission: a code of 10 or 20 with an acknowledgement code (a validateOnly call returns 10 and none, which accepts nothing).
func (r AISResponse) Accepted() bool {
	return (r.ReturnCode == AISSuccess || r.ReturnCode == AISWarnings) && r.Data.AcknowledgementCode != ""
}

// AISSubmitter sends a payload made by AISPayload. Production: the adapter that calls AIS-API 2.0 [live-verify]; the stub is for local mode and the tests.
type AISSubmitter interface {
	Submit(ctx context.Context, payload []byte) (AISResponse, error)
}

// AISStub is the stand-in for IRAS: it reads the payload as the sample request's shape, checks what IRAS's responses name (the counts and the total of A11 and A12, each
// person's total income against its parts, a record ID that runs from 1), and answers as section 3.8's samples do. It acknowledges with "AIS-" and the number of the
// submission it has taken, and never keeps what it is sent beyond a count.
type AISStub struct{ taken int }

var _ AISSubmitter = (*AISStub)(nil)

type stubRequest struct {
	Org struct {
		ValidateOnly   bool   `json:"validateOnly"`
		BypassWarning  bool   `json:"bypassWarning"`
		IncomeYear     int    `json:"incomeYear"`
		SubmissionType string `json:"submissionType"`
		TotalIR8A      int    `json:"totalNumberIR8A"`
		TotalIncome    int64  `json:"amountTotalIncomeOfIR8A"`
	} `json:"orgAndSubmissionInfo"`
	People []struct {
		RecordID int `json:"recordId"`
		IR8A     struct {
			Salary, Bonus, DirectorFees, Others *int64
			Total                               int64 `json:"totalIncome"`
		} `json:"ir8a"`
	} `json:"employeePersonalParticulars"`
}

// Submit implements AISSubmitter.
func (s *AISStub) Submit(_ context.Context, payload []byte) (AISResponse, error) {
	var r AISResponse
	r.Info.Warnings, r.Info.FieldInfo = []AISFieldInfo{}, []AISFieldInfo{}
	var q stubRequest
	if err := json.Unmarshal(payload, &q); err != nil {
		r.ReturnCode, r.Info.MessageCode, r.Info.Message = AISError, "850302", "Generic Error - Please contact API_SUPPORT@iras.gov.sg"
		return r, nil
	}
	bad := func(section, field, msg, rec string) {
		r.Info.FieldInfo = append(r.Info.FieldInfo, AISFieldInfo{Section: section, Field: field, Message: msg, RecordID: rec})
	}
	if len(q.People) != q.Org.TotalIR8A || q.Org.TotalIR8A == 0 {
		bad("OrgAndSubmissionInfo", "TotalNumberIR8A", "Must equal to the total number of IR8A records.", "NA")
	}
	var total int64
	for i, p := range q.People {
		if p.RecordID != i+1 {
			bad("EmployeePersonalParticulars", "RecordId", "Must be in unique numbers in running order starting from 1 and up to 2000.", fmt.Sprint(i+1))
		}
		total += p.IR8A.Total
		var parts int64
		for _, v := range []*int64{p.IR8A.Salary, p.IR8A.Bonus, p.IR8A.DirectorFees, p.IR8A.Others} {
			if v != nil {
				parts += *v
			}
		}
		if parts != p.IR8A.Total {
			bad("IR8A", "TotalIncome", "Must be the sum of Salary, Bonus, Director's Fee and Others Income.", fmt.Sprint(i+1))
		}
	}
	if total != q.Org.TotalIncome {
		bad("OrgAndSubmissionInfo", "AmountTotalIncomeOfIR8A", "Must equal to the total of Total Income in IR8A.", "NA")
	}
	if len(r.Info.FieldInfo) > 0 {
		r.ReturnCode, r.Info.MessageCode, r.Info.Message = AISError, "850301", "Arguments error"
		return r, nil
	}
	r.ReturnCode = AISSuccess
	if !q.Org.ValidateOnly { // a validation takes nothing and returns no acknowledgement code
		s.taken++
		r.Data.AcknowledgementCode = fmt.Sprintf("AIS-%010d", s.taken)
	}
	return r, nil
}

// Describe is IRAS's answer in plain words for the person who prepared the submission (ENG-23): what was taken, or each field to correct.
func (r AISResponse) Describe() string {
	switch {
	case r.Accepted():
		return "IRAS accepted the submission: acknowledgement code " + r.Data.AcknowledgementCode
	case r.ReturnCode == AISSuccess:
		return "IRAS validated the submission and took nothing (validateOnly)"
	}
	var parts []string
	for _, f := range append(append([]AISFieldInfo{}, r.Info.Warnings...), r.Info.FieldInfo...) {
		parts = append(parts, fmt.Sprintf("%s %s (record %s): %s", f.Section, f.Field, f.RecordID, f.Message))
	}
	if len(parts) == 0 {
		parts = append(parts, r.Info.Message)
	}
	return "IRAS did not take it: " + strings.Join(parts, "; ")
}
