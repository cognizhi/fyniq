# Application logs (§11.9, SEC-13; rev 0.244; p1-pdpa-retention D12): Cloud Logging's _Default bucket keeps what the
# services log for 30 days and then deletes it. The audit trail is not a log: it lives in the database and its anchors.
# Where the bucket stores logs, and that 30 days holds, is checked in the dev cloud [live-verify].
resource "google_logging_project_bucket_config" "application" {
  project        = var.project
  location       = "global"
  bucket_id      = "_Default"
  retention_days = 30
}
