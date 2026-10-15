# compliance

Controls as code (PRD-001 §13, Appendix F). The control registry, its runner and the evidence store
arrive with p0-compliance-as-code.

| File | What |
|---|---|
| `scan-waivers.toml` | The accepted findings of the security scans (`make scan`, `scripts/scan.py`; SEC-21, CTL-CHG-03): tool, rule, path, why and an expiry. Reviewed in the pull request that adds or renews a waiver; an expired waiver, or one that matches nothing any more, fails the scan |
