# req: CTL-DP-07
# req: CTL-IAM-04
# req: SEC-04
# req: CTL-DP-01

mock_provider "google" {}

variables {
  parent       = "organizations/123456789"
  cmek_parents = ["projects/fyniq-staging", "projects/fyniq-prod"]
}

run "the_policies_of_section_12_4" {
  command = plan
  assert {
    condition     = google_org_policy_policy.locations.spec[0].rules[0].values[0].allowed_values == tolist(["in:asia-southeast1-locations"])
    error_message = "resource locations must be Singapore only (CTL-DP-07)"
  }
  assert {
    condition     = contains(keys(google_org_policy_policy.enforced), "iam.disableServiceAccountKeyCreation") && google_org_policy_policy.enforced["iam.disableServiceAccountKeyCreation"].spec[0].rules[0].enforce == "TRUE"
    error_message = "service-account key creation must be refused (CTL-IAM-04)"
  }
  assert {
    condition     = contains(keys(google_org_policy_policy.enforced), "sql.restrictPublicIp")
    error_message = "Cloud SQL must not get a public IP"
  }
  assert {
    condition     = length(google_org_policy_policy.cmek) == 2 && contains(google_org_policy_policy.cmek["projects/fyniq-prod"].spec[0].rules[0].values[0].denied_values, "sqladmin.googleapis.com")
    error_message = "the database and storage must require CMEK in staging and production (CTL-DP-01)"
  }
}

run "a_project_is_not_a_parent" {
  command = plan
  variables { parent = "projects/fyniq-prod" }
  expect_failures = [var.parent]
}
