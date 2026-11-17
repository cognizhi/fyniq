// Package hostile holds QA-SEC-05's one fixture suite of hostile files, logos and exports (PRD-001 §16.2;
// p1-launch-readiness task 6.1). It has only tests: each fixture goes through the check the product applies to it
// (evidence.Check for an attached file, brand.Check for a logo, the sheet writer for an export) and must come out
// refused, neutralised or bounded as FR-DOC-08, FR-ORG-09 and FR-RPT-05 require.
package hostile
