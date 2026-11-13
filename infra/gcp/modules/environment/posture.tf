# The platform's posture, read by the jobs' account for the Compliance Center (SEC-33, CTL-RES-04, CTL-DP-03;
# p1-security-hardening design D8) [live-verify]: Security Command Center's findings of the project (Standard tier), and
# the services' application logs, which the weekly log scan samples. Read only: neither role changes a finding or a log,
# and logging.viewer reads no data-access audit log.
resource "google_project_iam_member" "posture_reader" {
  for_each   = toset(["roles/securitycenter.findingsViewer", "roles/logging.viewer"])
  project    = var.project
  role       = each.value
  member     = local.member["jobs"]
  depends_on = [google_service_account.all, google_project_service.apis]
}
