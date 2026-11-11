# req: NFR-OBS-02
# req: CTL-RES-05

mock_provider "google" {
  mock_resource "google_monitoring_notification_channel" {
    defaults = { id = "projects/fyniq-prod/notificationChannels/9" }
  }
}

variables {
  project        = "fyniq-prod"
  environment    = "production"
  owner_channels = ["projects/fyniq-prod/notificationChannels/1", "projects/fyniq-prod/notificationChannels/2"]
}

# The tick's absence tells the platform owner at 30 minutes and the Owners too at 60; the hourly jobs' at 3 hours; a late
# daily job, as heartbeats.check logs it, at once.
run "the_heartbeat_tells_the_platform_owner_then_the_owners" {
  command = plan
  variables {
    platform_owner_emails = ["platform@example.com"]
  }
  assert {
    condition = (google_monitoring_alert_policy.absent["tick"].conditions[0].condition_absent[0].duration == "1800s" &&
      google_monitoring_alert_policy.absent["tick"].notification_channels == tolist(["projects/fyniq-prod/notificationChannels/9"]) &&
      google_monitoring_alert_policy.absent["tick-escalated"].conditions[0].condition_absent[0].duration == "3600s" &&
    toset(google_monitoring_alert_policy.absent["tick-escalated"].notification_channels) == toset(["projects/fyniq-prod/notificationChannels/9", "projects/fyniq-prod/notificationChannels/1", "projects/fyniq-prod/notificationChannels/2"]))
    error_message = "the tick's absence: the platform owner at 30 minutes, the Owners as well at 60"
  }
  assert {
    condition = (strcontains(google_monitoring_alert_policy.absent["tick"].conditions[0].condition_absent[0].filter, "metric.label.job=\"tick\"") &&
      google_monitoring_alert_policy.absent["audit.anchor"].conditions[0].condition_absent[0].duration == "10800s" &&
    google_logging_metric.heartbeat.filter == "jsonPayload.event=\"fyniq.heartbeat\"")
    error_message = "absence alerts on the logged heartbeat, by job"
  }
  assert {
    condition = (google_logging_metric.missing.filter == "jsonPayload.event=\"fyniq.heartbeat_missing\"" &&
    google_monitoring_alert_policy.missing.conditions[0].condition_threshold[0].threshold_value == 0)
    error_message = "a late daily job alerts at its first line"
  }
}

run "without_a_named_platform_owner_the_owners_are_told_first" {
  command = plan
  assert {
    condition     = google_monitoring_alert_policy.absent["tick"].notification_channels == tolist(["projects/fyniq-prod/notificationChannels/1", "projects/fyniq-prod/notificationChannels/2"])
    error_message = "with no platform owner named, the Owners hear of the tick at 30 minutes"
  }
}
