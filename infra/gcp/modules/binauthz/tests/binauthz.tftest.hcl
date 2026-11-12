# req: SEC-22
# req: NFR-IMG-05
# req: CTL-CHG-04

mock_provider "google" {
  mock_data "google_project" {
    defaults = {
      number = "123456789"
    }
  }
  mock_data "google_kms_crypto_key_version" {
    defaults = {
      id = "//cloudkms.googleapis.com/v1/projects/fyniq-ci/locations/asia-southeast1/keyRings/release/cryptoKeys/attestor/cryptoKeyVersions/1"
      public_key = [{
        pem       = "-----BEGIN PUBLIC KEY-----\nMOCK\n-----END PUBLIC KEY-----\n"
        algorithm = "EC_SIGN_P256_SHA256"
      }]
    }
  }
  mock_resource "google_kms_crypto_key" {
    defaults = {
      id = "projects/fyniq-ci/locations/asia-southeast1/keyRings/release/cryptoKeys/attestor"
    }
  }
  mock_resource "google_binary_authorization_attestor" {
    defaults = {
      id = "projects/fyniq-ci/attestors/fyniq-release"
    }
  }
}

variables {
  project  = "fyniq-ci"
  attester = "serviceAccount:ci-attester@fyniq-ci.iam.gserviceaccount.com"
  registry = "projects/fyniq-ci/locations/asia-southeast1/repositories/images"
  environments = {
    staging    = { project = "fyniq-staging", enforce = true }
    production = { project = "fyniq-prod", enforce = true }
  }
}

# Every environment requires the release attestation for every image, with no allow-listed image, and every Cloud
# Run service must use that policy. The attester may sign and attach attestations, and nothing more.
run "only_attested_images_are_admitted" {
  command = plan
  assert {
    condition = alltrue([for p in google_binary_authorization_policy.environment :
      p.default_admission_rule[0].evaluation_mode == "REQUIRE_ATTESTATION" &&
      p.default_admission_rule[0].enforcement_mode == "ENFORCED_BLOCK_AND_AUDIT_LOG" &&
      length(p.default_admission_rule[0].require_attestations_by) == 1 &&
    length(p.admission_whitelist_patterns) == 0 && length(p.cluster_admission_rules) == 0])
    error_message = "an enforced policy must require the release attestation for every image, allow-list none, and have no other rule"
  }
  assert {
    condition     = google_binary_authorization_policy.environment["production"].project == "fyniq-prod" && google_binary_authorization_policy.environment["staging"].project == "fyniq-staging"
    error_message = "each environment's project has its own policy"
  }
  assert {
    condition = alltrue([for o in google_org_policy_policy.run_uses_default :
    o.spec[0].rules[0].values[0].allowed_values == tolist(["default"])])
    error_message = "every Cloud Run service must use the project's default policy, so none opts out"
  }
  assert {
    condition     = google_kms_crypto_key.attestor.purpose == "ASYMMETRIC_SIGN" && google_kms_crypto_key.attestor.version_template[0].algorithm == "EC_SIGN_P256_SHA256"
    error_message = "the attestor signs with an EC P-256 key in Cloud KMS"
  }
  assert {
    condition = toset([for m in google_kms_crypto_key_iam_member.attester : m.role]) == toset(["roles/cloudkms.signer", "roles/cloudkms.publicKeyViewer"]) && alltrue([
      google_container_analysis_note_iam_member.attester.role == "roles/containeranalysis.notes.attacher",
      google_binary_authorization_attestor_iam_member.attester.role == "roles/binaryauthorization.attestorsViewer",
      google_artifact_registry_repository_iam_member.attester.role == "roles/artifactregistry.reader",
    ])
    error_message = "the attester may sign, attach to the note, view the attestor and read the images; it may not write images or change the attestor"
  }
  assert {
    condition     = google_artifact_registry_repository_iam_member.attester.repository == "images" && google_artifact_registry_repository_iam_member.attester.location == "asia-southeast1"
    error_message = "the attester reads the release images' repository"
  }
  assert {
    condition = alltrue([for v in google_binary_authorization_attestor_iam_member.verifier :
    v.role == "roles/binaryauthorization.attestorsVerifier" && v.member == "serviceAccount:service-123456789@gcp-sa-binaryauthorization.iam.gserviceaccount.com"])
    error_message = "each environment's Binary Authorization agent verifies against the attestor"
  }
  assert {
    condition     = output.key_version == "projects/fyniq-ci/locations/asia-southeast1/keyRings/release/cryptoKeys/attestor/cryptoKeyVersions/1"
    error_message = "the key version is what the release workflow passes to gcloud's --keyversion"
  }
}

# Before the first attested release replaces the placeholder image, an environment's policy only logs.
run "a_dry_run_only_logs" {
  command = plan
  variables {
    environments = { staging = { project = "fyniq-staging", enforce = false } }
  }
  assert {
    condition     = google_binary_authorization_policy.environment["staging"].default_admission_rule[0].enforcement_mode == "DRYRUN_AUDIT_LOG_ONLY"
    error_message = "a policy that does not enforce must still log every image it would refuse"
  }
}
