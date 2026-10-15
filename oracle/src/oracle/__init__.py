"""Fyniq's independent reference ledger (QA-ENG-01).

A second implementation of the posting rules, written from PRD-001 §10 and Appendix B, never from
the Go engine, so that the differential tests (QA-ENG-02) can catch either one's mistakes. It ports
the workbook's reference ledger (``tests/reference.py`` in ``zh-sheet-accounting``) to exact
decimals. See ``oracle/README.md``.
"""
