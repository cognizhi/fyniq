package bankfile

import (
	"bytes"
	"encoding/xml"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// camt.053 (ISO 20022 BankToCustomerStatement, any version: elements are matched by their local names): one or more
// Stmt, each with its account, its balances (OPBD the opening, CLBD the closing) and its entries; only booked entries
// (BOOK) are read. Go's XML decoder resolves no external entity and reads no DTD, so the file cannot reach outside
// itself; its size is bounded before it is read.

type camtAmount struct {
	Value string `xml:",chardata"`
	Ccy   string `xml:"Ccy,attr"`
}

type camtDate struct {
	Dt   string `xml:"Dt"`
	DtTm string `xml:"DtTm"`
}

func (d camtDate) day() string {
	if d.Dt != "" {
		return d.Dt
	}
	if len(d.DtTm) >= 10 {
		return d.DtTm[:10]
	}
	return d.DtTm
}

type camtBalance struct {
	Code string     `xml:"Tp>CdOrPrtry>Cd"`
	Amt  camtAmount `xml:"Amt"`
	Ind  string     `xml:"CdtDbtInd"`
	Dt   camtDate   `xml:"Dt"`
}

// camtParty is a related party: its name directly (version 2) or under Pty (version 8 and later).
type camtParty struct {
	Name    string `xml:"Nm"`
	PtyName string `xml:"Pty>Nm"`
}

func (p camtParty) name() string {
	if p.Name != "" {
		return p.Name
	}
	return p.PtyName
}

type camtTx struct {
	EndToEnd string    `xml:"Refs>EndToEndId"`
	Debtor   camtParty `xml:"RltdPties>Dbtr"`
	Creditor camtParty `xml:"RltdPties>Cdtr"`
	Ustrd    []string  `xml:"RmtInf>Ustrd"`
}

// camtStatus is an entry's status: the text itself in older versions, a Cd child in newer ones.
type camtStatus struct {
	Text string `xml:",chardata"`
	Cd   string `xml:"Cd"`
}

type camtEntry struct {
	Amt      camtAmount `xml:"Amt"`
	Ind      string     `xml:"CdtDbtInd"`
	Sts      camtStatus `xml:"Sts"`
	Booked   camtDate   `xml:"BookgDt"`
	Value    camtDate   `xml:"ValDt"`
	Ref      string     `xml:"AcctSvcrRef"`
	Txs      []camtTx   `xml:"NtryDtls>TxDtls"`
	AddlInfo string     `xml:"AddtlNtryInf"`
}

type camtStatement struct {
	IBAN     string        `xml:"Acct>Id>IBAN"`
	Other    string        `xml:"Acct>Id>Othr>Id"`
	Ccy      string        `xml:"Acct>Ccy"`
	From     string        `xml:"FrToDt>FrDtTm"`
	To       string        `xml:"FrToDt>ToDtTm"`
	Balances []camtBalance `xml:"Bal"`
	Entries  []camtEntry   `xml:"Ntry"`
}

type camtDocument struct {
	Statements []camtStatement `xml:"BkToCstmrStmt>Stmt"`
}

func camtMoney(a camtAmount, ind string, cur money.Currency, line int, field string) (money.Money, error) {
	if a.Ccy != "" && !strings.EqualFold(a.Ccy, cur.Code) {
		return money.Money{}, refuse(CodeCurrency, line, field, "Import each currency's statement on its own.", "an amount in %s in a statement in %s", a.Ccy, cur.Code)
	}
	m, err := amount(a.Value, Number{}, cur, line, field)
	if err != nil {
		return m, err
	}
	if ind == "DBIT" {
		return m.Neg(), nil
	}
	return m, nil
}

func parseCAMT053(data []byte, lim Limits) ([]Statement, error) {
	var doc camtDocument
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	if err := dec.Decode(&doc); err != nil {
		return nil, refuse(CodeFormat, 0, "", "Export the statement as camt.053 XML from the bank.", "the file is not camt.053 XML: %v", err)
	}
	if len(doc.Statements) == 0 {
		return nil, refuse(CodeFormat, 0, "Stmt", "Export the statement as camt.053 XML from the bank.", "the file has no statement")
	}
	var out []Statement
	lines := 0
	for si, st := range doc.Statements {
		code := st.Ccy
		if code == "" {
			for _, b := range st.Balances {
				if b.Amt.Ccy != "" {
					code = b.Amt.Ccy
					break
				}
			}
		}
		cur, err := currency(code, si+1, "Acct>Ccy")
		if err != nil {
			return nil, err
		}
		s := Statement{Currency: cur}
		account := st.IBAN
		if account == "" {
			account = st.Other
		}
		if s.Account, err = text(account, si+1, "Acct>Id", lim); err != nil {
			return nil, err
		}
		for _, b := range st.Balances {
			if b.Code != "OPBD" && b.Code != "CLBD" && b.Code != "PRCD" {
				continue
			}
			m, err := camtMoney(b.Amt, b.Ind, cur, si+1, "Bal "+b.Code)
			if err != nil {
				return nil, err
			}
			on, err := date(b.Dt.day(), []string{"2006-01-02"}, si+1, "Bal "+b.Code)
			if err != nil {
				return nil, err
			}
			switch {
			case (b.Code == "OPBD" || b.Code == "PRCD") && s.Opening == nil:
				s.Opening, s.From = &m, on
			case b.Code == "CLBD":
				s.Closing, s.To = &m, on
			}
		}
		if len(st.From) >= 10 {
			if s.From, err = date(st.From[:10], []string{"2006-01-02"}, si+1, "FrToDt"); err != nil {
				return nil, err
			}
		}
		if len(st.To) >= 10 {
			if s.To, err = date(st.To[:10], []string{"2006-01-02"}, si+1, "FrToDt"); err != nil {
				return nil, err
			}
		}
		for ei, e := range st.Entries {
			status := strings.TrimSpace(e.Sts.Cd)
			if status == "" {
				status = strings.TrimSpace(e.Sts.Text)
			}
			if status != "" && status != "BOOK" {
				continue // pending and informational entries are not statement lines
			}
			lines++
			if lines > lim.MaxLines {
				return nil, refuse(CodeTooLarge, ei+1, "Ntry", "Split the statement by month.", "the file has more than %d entries", lim.MaxLines)
			}
			amt, err := camtMoney(e.Amt, e.Ind, cur, ei+1, "Ntry>Amt")
			if err != nil {
				return nil, err
			}
			booked, err := date(e.Booked.day(), []string{"2006-01-02"}, ei+1, "Ntry>BookgDt")
			if err != nil {
				return nil, err
			}
			l := Line{Position: len(s.Lines) + 1, Booked: booked, Amount: amt}
			if d := e.Value.day(); d != "" && d != e.Booked.day() {
				v, err := date(d, []string{"2006-01-02"}, ei+1, "Ntry>ValDt")
				if err != nil {
					return nil, err
				}
				l.Value = &v
			}
			if l.Reference, err = text(e.Ref, ei+1, "Ntry>AcctSvcrRef", lim); err != nil {
				return nil, err
			}
			var party string
			var desc []string
			for _, tx := range e.Txs {
				p := tx.Creditor.name()
				if e.Ind == "CRDT" {
					p = tx.Debtor.name()
				}
				if party == "" {
					party = p
				}
				desc = append(desc, tx.Ustrd...)
				if tx.EndToEnd != "" && tx.EndToEnd != "NOTPROVIDED" {
					desc = append(desc, tx.EndToEnd)
				}
			}
			if len(desc) == 0 {
				desc = append(desc, e.AddlInfo)
			}
			if l.Counterparty, err = text(party, ei+1, "RltdPties", lim); err != nil {
				return nil, err
			}
			if l.Description, err = text(strings.Join(desc, " "), ei+1, "RmtInf", lim); err != nil {
				return nil, err
			}
			s.Lines = append(s.Lines, l)
		}
		if s.From.IsZero() || s.To.IsZero() {
			span(&s)
		}
		out = append(out, s)
	}
	return out, nil
}
