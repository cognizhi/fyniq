package letters

// KETDefault is the standard text of the key employment terms letter: the seventeen items of MOM's tripartite guidelines in their order, each with the field that fills it
// (NM-LTR-06). A company starts from it and may change the wording; the fields are the same.
var KETDefault = []Block{
	{Kind: Letterhead, Text: "{{company.name}}\nUEN {{company.uen}}\n{{company.address}}"},
	{Kind: Heading, Text: "Key employment terms"},
	{Kind: Paragraph, Text: "Reference {{letter.reference}}. Dated {{letter.date}}."},
	{Kind: Paragraph, Text: "1. Employer: {{company.name}}\n2. Employee: {{person.legal_name}}\n3. Job title and main duties and responsibilities: {{job.title}}. {{terms.main_duties}}\n4. Date of commencing employment: {{employment.start}}\n5. Duration of employment (fixed-term contracts only): {{terms.contract_duration}}"},
	{Kind: Paragraph, Text: "6. Working hours, working days per week and rest days: {{terms.working_arrangement}}\n7. Salary period: {{terms.salary_period}}\n8. Basic salary per salary period: {{terms.basic_salary}}\n9. Fixed allowances per salary period: {{terms.fixed_allowances}}\n10. Fixed deductions per salary period: {{terms.fixed_deductions}}"},
	{Kind: Paragraph, Text: "11. Overtime payment period: {{terms.overtime_period}}\n12. Overtime rate of pay: {{terms.overtime_rate}}\n13. Other salary-related components: {{terms.other_pay}}\n14. Leave entitlements: {{terms.leave}}\n15. Other medical benefits: {{terms.medical_benefits}}\n16. Probation period: {{terms.probation}}\n17. Notice period for termination of employment initiated by either party: {{terms.notice_period}}"},
	{Kind: Signature, Text: "For {{company.name}}"},
}
