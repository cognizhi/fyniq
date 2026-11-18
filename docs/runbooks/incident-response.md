# Runbook: a security incident

**When:** a security alert fires (SEC-31: an admin's sign-in from a new device or impossible travel, a vendor's bank
details changed, a mass export, repeated refusals, a failed audit-chain verification, the platform's logs holding a
secret or an NRIC, an AI red-team pattern), a Cloud Monitoring policy emails the founders (`infra/gcp/modules/
environment/alerts.tf`), a control fails in the Compliance Center, a customer, a supplier or a researcher reports
something, or anyone suspects that data, money or access was misused. **Requirements:** PRD-001 SEC-32, CMP-PD-05,
CTL-DP-06; `p1-security-hardening` design D10.

**Open an incident first, then work it.** The register is **Compliance › Controls › Incidents**. Its clock is the
evidence: what was known, decided and done, and when. Steps are only ever added. The DPO keeps it (Appendix D,
Compliance Center: C/A), so a founder who responds holds the DPO role; the Owners, the Administrators, the
Controller, the Accountant and the Auditor read it. Write what you saw, not what you
guess. If personal data may be involved, record a breach in **Compliance › Data protection › Breaches** too: the
PDPA's 30 days run from when the company became aware of it.

| Step | Who | In the register |
|---|---|---|
| 1. Detect and triage | Whoever sees it; the Owner decides the severity | **Open** the incident |
| 2. Contain | The Owner, or a Super admin for the installation | **Contained** |
| 3. Preserve evidence | The Owner | (a note on the step that preserved it) |
| 4. Assess | The Owner and the DPO | **Assessed**, linked to the breach if personal data was involved |
| 5. Notify | The DPO (PDPC and people); the Owner (customers, partners) | **Notified** |
| 6. Recover | The Owner, through the normal pipeline | **Recovered** |
| 7. Learn | The Owner and the DPO | **Reviewed**: the incident closes |

Roles, not names: the Owner and the DPO of the organisation; the Super admins for the installation (the deployment,
the cloud projects, the installation connectors). Before production both founders hold the Owner and Super admin
roles; either may run any step, and the other reviews.

## 1. Detect and triage

1. Read the alert where it landed: the notification centre (a locked critical notice), the founders' email from
   Cloud Monitoring, or the Compliance Center. The audit event `security.alerted` says the rule and its facts; the
   log entry `fyniq.security_alert` is in the security log bucket (`fyniq-<environment>-security`, kept 400 days).
2. **Open the incident** with its severity:
   - **Critical**: money moved or about to move wrongly, personal data left the company, an attacker holds a
     session, a key or an admin role, or the ledger's integrity is in doubt (a failed audit-chain verification).
   - **High**: a credible attempt that was stopped, or exposure without evidence of access.
   - **Medium**: a control failing, a vulnerability with no known exploitation.
   - **Low**: a false alarm worth recording, such as a founder's own new laptop.
3. A Critical or High incident: call the other founder now (below), whatever the hour.

## 2. Contain

Stop the harm first; fix it later. Each tool, and who may use it:

| Threat | Contain with |
|---|---|
| A member's account is misused | **Settings › Members**: remove the member (`removeMember`) or revoke the role (`revokeRole`); their next request is refused. The person signs out their other devices themselves (`signOutOtherDevices`) once they have their account back |
| A passkey or a device is lost | **Settings › Profile and security**: revoke the passkey, sign out the device |
| A Super admin is locked out or compromised | `docs/runbooks/super-admin-recovery.md` (two operators) |
| A connector or an integration misbehaves | **Settings › Integrations**: **Pause** the connector (at once, no second person) |
| A document's online link leaked | Revoke the link (`revokeDocumentLink`) |
| A payment batch is wrong | Do not release it; a released one cannot be recalled by Fyniq: call the bank |
| A vendor's bank details were changed by an attacker | Reject the pending bank change: it waits for another person's approval, and the old details stay in force until one is approved |
| A secret leaked (an API key, a connector secret) | Rotate it at its issuer, then set the new value (`setInstallationConnectorSecret` / `setOrganisationConnectorSecret`; the platform's own in Secret Manager through OpenTofu). Never paste it in the incident |
| An address or a country attacks the edge | Add a Cloud Armor deny rule to the `edge` module and apply it; the per-IP limits already ban sign-in floods |
| A bad image or a compromised build | Binary Authorization admits only attested images: stop the release (do not merge the release pull request), roll back to the previous revision (Cloud Run keeps it) |
| Someone needs production access to contain it | Break-glass through Privileged Access Manager (CTL-IAM-03): requested, approved by the other founder, time-limited, recorded |
| The AI or an MCP client misbehaves | The AI's and each MCP client's kill switch (AI-13, AI-43) arrive with `p1-ai-assistant`; until then no AI service runs in production |

Record **Contained** with what you did. Leave the attacker's data where it is: removing a member keeps their history.

## 3. Preserve evidence

- **Never alter the trail.** The audit trail is a hash chain anchored every hour; verify it on demand
  (**Compliance › Audit trail**, **Verify integrity**; `getAuditVerification`). Export the events of the window
  (`requestAuditTrailExport`); the export names the version and the commit it was prepared by.
- Cloud Audit Logs and the security alerts are in the security log bucket in `fyniq-audit` (locked, 400 days).
- Screenshots and files go on the incident's steps as notes, or into the evidence store under the evidence rules
  (FR-DOC-05…07). Never paste a secret, a token, a full NRIC or a bank number (SEC-15).
- A database restore is itself an incident: `docs/runbooks/audit-restore.md`.

## 4. Assess

Answer, and record them on **Assessed**:

1. **Was personal data involved?** If yes, record a breach (**Compliance › Data protection › Breaches**) and link it
   to the incident. The breach register keeps the PDPA's clock: the assessment is due **30 days** after the company
   became aware of it.
2. **Is it notifiable?** A breach is notifiable when it is likely to cause significant harm, or affects **500 or
   more** people. A person's name or identification number with their pay, card or bank account numbers, or an
   account's identifier with its password, is deemed to cause significant harm (the Notification of Data Breaches
   Regulations 2021, reg 3 and its Schedule); the register refuses to call such a breach not notifiable.
3. **Is the ledger intact?** Run the nightly verification and the audit-chain verification on demand. Wrong figures
   are corrected only by approved journals or reversals, never by editing posted history (FR-GL-12).
4. **What else is exposed?** Other organisations of the installation, other keys, other members.

## 5. Notify

- **The PDPC**, within **3 calendar days** after the breach is decided notifiable: the breach register shows the
  due day and alerts when it passes. Notify through the PDPC's data breach notification form [verify the channel at
  https://www.pdpc.gov.sg before you need it], then record **Notified the PDPC** with its reference in the breach
  register.
- **The people affected**, as soon as practicable, once the PDPC is notified or with it, unless an exception of the
  Act applies [counsel to confirm the exceptions]: record **Told the people affected** in the breach register.
- **Customers and partners**, when their data or money is involved: the Owner, in writing.
- **As a data intermediary** (P3: the platform for other companies), tell each affected customer without undue delay.
- **A cyber attack** may be reported to SingCERT (Cyber Security Agency of Singapore) [verify the channel].

Record **Notified** on the incident with who was told and when.

## 6. Recover

- Fixes go through the normal pipeline: a branch, a pull request with its tests (a regression test for the cause),
  a release. No hand edits in production; no standing access (SEC-03).
- Restore a service by rolling back to its previous Cloud Run revision, or roll forward with the fix.
- Data corrections only through approved journals or reversals; a restore from a backup follows
  `docs/runbooks/audit-restore.md`.
- Rotate every secret the attacker could have read, not only the one you know leaked.

Record **Recovered** with what was restored and when.

## 7. Learn

Within two weeks, the Owner and the DPO hold the root-cause review:

- what happened, why the controls let it, and what changes: a new test, a new control, a changed threshold
  (`FYNIQ_ALERT_THRESHOLDS`), an updated threat model (`docs/security/threat-models/`);
- a risk in the risk register (**Compliance › Risks**) if one remains;
- this runbook, if a step was missing or wrong.

Record **Reviewed** with the review's findings: the incident closes.

## Rehearse it

The breach runbook is rehearsed in a tabletop exercise twice a year (CTL-DP-06): **Compliance › Data protection ›
Breaches › Record an exercise**, which the review `breach-tabletop` tracks. Use one of the alerts above as the
scenario, and walk every step of this runbook.

## Who to call

| Who | When | How |
|---|---|---|
| The other founder | Every Critical or High incident, at once | Phone first, then the incident's notes |
| The DPO | Personal data may be involved | As named in **Compliance › Data protection › Data Protection Officer** |
| The PDPC | A notifiable breach, within 3 calendar days of the decision | Its data breach notification form [verify] |
| The bank | A payment released wrongly, or a bank account misused | The bank's fraud line, from the bank connection's page |
| Google Cloud support | The cloud itself is involved | The support case in the Cloud console [live-verify the plan] |
| Counsel | Before telling people affected, when liability is possible | [counsel to confirm] |
