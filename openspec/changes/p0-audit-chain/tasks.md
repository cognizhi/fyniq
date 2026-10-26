# Tasks

## 1. Schema and writing

- [x] 1.1 Define the AUD-01 event schema (versioned, documented in `contracts/`) and the audit table with RLS; test `// req: AUD-01` validates every emitted event against the schema
- [x] 1.2 Make the unit of work append the audit event in the same transaction, and add the deferred trigger that rejects audited changes without one; attack test `// req: AUD-08` writes through raw SQL without an event and expects the commit to fail
- [x] 1.3 Compute the per-organisation hash chain under the fixed lock order; test `// req: INV-12` tampers with one stored event and expects verification to fail
- [x] 1.4 Mask sensitive fields in `changes` and in logs; test `// req: SEC-15` asserts no bank number, NRIC or token appears in events or log output

## 2. Anchors and verification

- [ ] 2.1 [live-verify] Add the `Signer` port (KMS adapter, local Ed25519 adapter) and the hourly anchor job writing to the WORM bucket; test `// req: CTL-AUD-02` checks the signature and the retention lock locally (fake GCS) and in staging (15 Oct 2026: built and tested locally: `internal/adapters/gcp.KMS` signs with an EC P-256 key in Cloud KMS through its REST API, with CRC32C checks, and verifies every version of the key, tested against a fake; `gcp.Storage` is the BlobStore on Cloud Storage's JSON API, write-once with `ifGenerationMatch=0`, tested against fake-gcs-server, and reads the bucket's retention lock. The staging run, with the locked bucket, is held until p0-infra-opentofu deploys it)
- [ ] 2.2 [live-verify] Add the nightly verification job for all organisations, raising an alert and a ✖ check on mismatch; test `// req: AUD-03` runs it after normal writes (pass) and after tampering (fail) (15 Oct 2026: the job and its test are built: `audit.VerifyAll` records the outcome in each trail and fails the job, which alerts; the ✖ in the checks catalogue waits for the Checks module, P1)
- [ ] 2.3 [live-verify] Prove §17.3 exit item 3 in staging (chain written, verified nightly, anchored to the locked bucket) and store the evidence in the release record, then `make check` and `make test`
