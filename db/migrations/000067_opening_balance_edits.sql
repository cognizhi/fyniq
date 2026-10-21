-- Changes to posted opening balances (PRD-001 FR-GL-06, FR-GL-12, FM-1; rev 0.66; p1-general-ledger-close tasks 2.3
-- and 3.1).
--
-- Posted opening balances change only as an edit of their document (000061: the next version, with the reversal of the
-- entry it had and the new version's entry, in one transaction, which document_edit_posted checks at commit), and only
-- on another person's approval in every team mode, as FM-1 lists "editing opening balances" among the high-risk
-- actions. The approval engine gains the object `opening_balance` and its operation `opening_balance.edit`, and an
-- organisation's policy for it. The widened checks are added NOT VALID and validated by 000068 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'))) NOT VALID;
ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance')) NOT VALID;
