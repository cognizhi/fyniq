# oracle

The independent reference ledger (QA-ENG-01): a second implementation of the posting rules in
Python, written from PRD-001 §10 and Appendix B, never translated from the Go engine. The
differential tests require the engine and the oracle to agree to the cent (QA-ENG-02, golden
rule 3). Whoever writes it must not read `internal/domain/ledger`. It arrives with
p0-python-oracle-differential.
