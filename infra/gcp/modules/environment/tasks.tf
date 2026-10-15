# Background work (ADR-T7, §12.11): one Cloud Tasks queue per worker class, each class's handler invoked
# only by that class's invoker account, and the Scheduler tick. Only the api's account may create tasks
# and act as an invoker, so no other identity can send the executor work (Proposal executor queue
# isolation); the executor still runs only what it finds approved in the database (§9.3).

resource "google_cloud_tasks_queue" "classes" {
  for_each = local.spec.classes
  project  = var.project
  name     = each.key
  location = local.region
  rate_limits {
    max_concurrent_dispatches = each.key == "executor" ? 1 : 10
    max_dispatches_per_second = 5
  }
  retry_config {
    max_attempts  = 5 # the stated maximum; after it the row is dead and shows on Checks (C.2 #12)
    min_backoff   = "10s"
    max_backoff   = "300s"
    max_doublings = 4
  }
  stackdriver_logging_config {
    sampling_ratio = 1.0
  }
  depends_on = [google_project_service.apis]
}

resource "google_cloud_tasks_queue_iam_member" "enqueuer" {
  for_each = google_cloud_tasks_queue.classes
  project  = var.project
  location = local.region
  name     = each.value.name
  role     = "roles/cloudtasks.enqueuer"
  member   = local.member["api"]
}

resource "google_service_account_iam_member" "act_as_invoker" {
  for_each           = local.spec.classes
  service_account_id = "projects/${var.project}/serviceAccounts/${local.email["tasks-${each.key}"]}"
  role               = "roles/iam.serviceAccountUser"
  member             = local.member["api"]
  depends_on         = [google_service_account.all]
}

resource "google_cloud_run_v2_service_iam_member" "invoker" {
  for_each = local.spec.classes
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_service.services[each.value.service].name
  role     = "roles/run.invoker"
  member   = local.member["tasks-${each.key}"]
}

# The tick wakes the api every few minutes (§12.11), with an OIDC token for /internal/tick.
resource "google_cloud_scheduler_job" "tick" {
  project   = var.project
  name      = "tick"
  region    = local.region
  schedule  = local.spec.tick_cron
  time_zone = "Asia/Singapore"
  http_target {
    http_method = "POST"
    uri         = "${local.url["api"]}/internal/tick"
    oidc_token {
      service_account_email = local.email["scheduler"]
      audience              = "${local.url["api"]}/internal/tick"
    }
  }
  retry_config {
    retry_count = 0 # the next tick is the retry
  }
  depends_on = [google_service_account.all, google_project_service.apis]
}

# Long work (§12.11 item 4): the api's tick and the worker start the jobs image as a Cloud Run job.
resource "google_cloud_run_v2_job_iam_member" "run_jobs" {
  for_each = toset(["api", "worker"])
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_job.jobs["jobs"].name
  role     = "roles/run.jobsExecutorWithOverrides"
  member   = local.member[each.value]
}
