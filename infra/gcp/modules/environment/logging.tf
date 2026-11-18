# Application logs (§11.9, SEC-13; rev 0.244; p1-pdpa-retention D12): Cloud Logging's _Default bucket keeps what the
# services log for 30 days and then deletes it. The audit trail is not a log: it lives in the database and its anchors.
# Where the bucket stores logs, and that 30 days holds, is checked in the dev cloud [live-verify].
resource "google_logging_project_bucket_config" "application" {
  project        = var.project
  location       = "global"
  bucket_id      = "_Default"
  retention_days = 30
}

# Each of Fyniq's own services writes its traces to Cloud Trace through the Telemetry API, and nothing else of it
# (NFR-OBS-01) [verify the role's name] [live-verify], once var.traces is on (CTL-DP-07, rev 0.250).
resource "google_project_iam_member" "traces" {
  for_each = local.traced
  project  = var.project
  role     = "roles/telemetry.tracesWriter"
  member   = local.member[each.key]
}
