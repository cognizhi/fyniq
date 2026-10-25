# req: SEC-03
# req: SEC-12
# req: CTL-DP-01
# req: CTL-IAM-03
# req: CTL-CHG-05
# req: NFR-PORT-05

mock_provider "google" {
  mock_data "google_project" {
    defaults = { number = "123456789012" }
  }
  mock_resource "google_kms_crypto_key" {
    defaults = { id = "projects/fyniq-staging/locations/asia-southeast1/keyRings/platform-staging/cryptoKeys/mock" }
  }
  mock_resource "google_compute_network" {
    defaults = { id = "projects/fyniq-staging/global/networks/platform" }
  }
}

mock_provider "google-beta" {
  mock_resource "google_project_service_identity" {
    defaults = {
      email  = "service-123456789012@gcp-sa-cloud-sql.iam.gserviceaccount.com"
      member = "serviceAccount:service-123456789012@gcp-sa-cloud-sql.iam.gserviceaccount.com"
    }
  }
}

run "spec" {
  command = apply # the spec has no resources: this only computes it
  module {
    source = "../../../modules/platform-spec"
  }
  variables {
    environment = "staging"
    budget      = { amount_usd = 120, emails = ["founders@example.com"] }
    anchors     = { retention_days = 30, lock = false }
  }
}

variables {
  project       = "fyniq-staging"
  public_origin = "https://staging.fyniq.example"
  anchors = {
    bucket      = "fyniq-staging-audit-anchors"
    key_version = "projects/fyniq-staging/locations/asia-southeast1/keyRings/audit-staging/cryptoKeys/anchor/cryptoKeyVersions/1"
  }
  break_glass = {
    requesters = ["group:platform-admins@example.com"]
    approvers  = ["group:founders@example.com"]
    role       = "roles/editor"
    duration   = "3600s"
  }
}

run "a_public_origin_is_https_without_a_path" {
  command = plan
  variables {
    spec          = run.spec.spec
    public_origin = "http://staging.fyniq.example/app"
  }
  expect_failures = [var.public_origin]
}

# One service account per service and job; only the services with a database role read a database
# secret; the MCP server and the AI orchestrator read none.
run "each_service_is_its_own_identity" {
  command = plan
  variables { spec = run.spec.spec }
  assert {
    condition     = length(distinct(values(output.accounts))) == length(output.accounts) && length(output.accounts) == 15
    error_message = "8 services, 3 jobs, 3 invokers and the Scheduler: each its own account (SEC-03)"
  }
  assert {
    condition     = !anytrue([for a in google_secret_manager_secret_iam_member.access : contains(["serviceAccount:run-mcp@fyniq-staging.iam.gserviceaccount.com", "serviceAccount:run-ai@fyniq-staging.iam.gserviceaccount.com"], a.member)])
    error_message = "the MCP server and the AI orchestrator must read no secret and reach no database (§12.3)"
  }
  assert {
    condition     = google_secret_manager_secret_iam_member.access["executor-database-url/executor"].member == "serviceAccount:run-executor@fyniq-staging.iam.gserviceaccount.com" && !contains(keys(google_secret_manager_secret_iam_member.access), "executor-database-url/worker")
    error_message = "each database secret is read by its own service only"
  }
  assert {
    condition     = alltrue([for s in google_cloud_run_v2_service.services : s.template[0].scaling[0].min_instance_count == 0])
    error_message = "every service scales to zero (ADR-T7)"
  }
  assert {
    condition     = google_cloud_run_v2_service.services["executor"].ingress == "INGRESS_TRAFFIC_INTERNAL_ONLY" && google_cloud_run_v2_service.services["oauth-admin"].ingress == "INGRESS_TRAFFIC_INTERNAL_ONLY"
    error_message = "the executor and Hydra's admin port take internal traffic only"
  }
  assert {
    condition     = toset(keys(google_cloud_run_v2_service_iam_member.public)) == toset(["api", "ai", "mcp", "oauth", "oauth-admin"])
    error_message = "only the public services, and Hydra's admin port inside the network, may be invoked without a Google identity"
  }
  assert {
    condition     = !contains(keys(google_cloud_run_v2_service_iam_member.public), "executor") && !contains(keys(google_cloud_run_v2_service_iam_member.public), "worker")
    error_message = "the worker and the executor are invoked only by their invokers' Google identities"
  }
}

# Proposal executor queue isolation (§12.11): only the api's account creates tasks or acts as an
# invoker, and the executor is invoked only by its own invoker, which no other class shares.
run "only_the_api_sends_the_executor_work" {
  command = plan
  variables { spec = run.spec.spec }
  assert {
    condition     = alltrue([for m in google_cloud_tasks_queue_iam_member.enqueuer : m.member == "serviceAccount:run-api@fyniq-staging.iam.gserviceaccount.com" && m.role == "roles/cloudtasks.enqueuer"])
    error_message = "only the api may create tasks"
  }
  assert {
    condition     = alltrue([for m in google_service_account_iam_member.act_as_invoker : m.member == "serviceAccount:run-api@fyniq-staging.iam.gserviceaccount.com"])
    error_message = "only the api may act as an invoker"
  }
  assert {
    condition     = google_cloud_run_v2_service_iam_member.invoker["executor"].name == "executor" && google_cloud_run_v2_service_iam_member.invoker["executor"].member == "serviceAccount:tasks-executor@fyniq-staging.iam.gserviceaccount.com"
    error_message = "the executor is invoked by its own invoker"
  }
  assert {
    condition     = length([for k, m in google_cloud_run_v2_service_iam_member.invoker : k if m.name == "executor"]) == 1
    error_message = "no other invoker may call the executor"
  }
  assert {
    condition     = google_cloud_tasks_queue.classes["executor"].retry_config[0].max_attempts == 5
    error_message = "the queues retry a stated number of times (§12.11)"
  }
}

# FYNIQ_GCP is exactly what internal/wiring/release.go checks: every class's queue, handler and
# invoker, the Scheduler, the anchors and the jobs image's job, with the executor's invoker its own.
run "the_wiring_the_services_read" {
  command = plan
  variables { spec = run.spec.spec }
  assert {
    condition     = toset(keys(jsondecode(output.gcp_config).tasks)) == toset(["worker", "executor", "jobs"])
    error_message = "every worker class has its queue"
  }
  assert {
    condition     = jsondecode(output.gcp_config).tasks.executor.queue == "projects/fyniq-staging/locations/asia-southeast1/queues/executor" && startswith(jsondecode(output.gcp_config).tasks.executor.url, "https://executor-123456789012.asia-southeast1.run.app")
    error_message = "the executor's queue and handler"
  }
  assert {
    condition     = jsondecode(output.gcp_config).invokers.executor.service_account != jsondecode(output.gcp_config).invokers.worker.service_account
    error_message = "the executor must not accept the worker's invoker"
  }
  assert {
    condition     = jsondecode(output.gcp_config).invokers.scheduler.audience == "https://api-123456789012.asia-southeast1.run.app/internal/tick" && google_cloud_scheduler_job.tick.http_target[0].oidc_token[0].audience == jsondecode(output.gcp_config).invokers.scheduler.audience
    error_message = "the tick's token is for /internal/tick, as the api checks"
  }
  assert {
    condition     = jsondecode(output.gcp_config).job == "projects/fyniq-staging/locations/asia-southeast1/jobs/jobs" && jsondecode(output.gcp_config).anchors.bucket == "fyniq-staging-audit-anchors"
    error_message = "the jobs image's job and the anchors"
  }
  assert {
    condition     = local.env["api"]["FYNIQ_PUBLIC_ORIGIN"] == "https://staging.fyniq.example" && local.env["api"]["FYNIQ_SIGNIN"] == "identity-platform" && local.secret_env["api"]["FYNIQ_IDP_API_KEY"] == "idp-api-key"
    error_message = "the api signs in through Identity Platform at the public origin (cmd/api/release.go)"
  }
  assert {
    condition     = local.env["oauth"]["URLS_SELF_ISSUER"] == "https://staging.fyniq.example/oauth/" && local.env["oauth"]["OAUTH2_PKCE_ENFORCED"] == "true" && !contains(local.args["oauth"], "--dev")
    error_message = "Hydra: an https issuer, PKCE enforced, never --dev (ADR-T3, AI-36)"
  }
  assert {
    condition     = google_cloud_scheduler_job.tick.schedule == "*/5 * * * *" && google_cloud_scheduler_job.tick.time_zone == "Asia/Singapore"
    error_message = "the tick runs every 5 minutes"
  }
  assert {
    condition     = jsondecode(output.gcp_config).render.url == "https://render-123456789012.asia-southeast1.run.app" && jsondecode(output.gcp_config).render.audience == jsondecode(output.gcp_config).render.url && toset(jsondecode(output.gcp_config).render.service_accounts) == toset(["run-api@fyniq-staging.iam.gserviceaccount.com", "run-worker@fyniq-staging.iam.gserviceaccount.com"])
    error_message = "the render service accepts the api's and the worker's ID tokens for its own audience (§12.3, rev 0.96)"
  }
}

# The render service is private: no public invoker, and only the api's and the worker's accounts may call it.
run "only_the_api_and_the_worker_render" {
  command = plan
  variables { spec = run.spec.spec }
  assert {
    condition     = !contains(keys(google_cloud_run_v2_service_iam_member.public), "render") && google_cloud_run_v2_service.services["render"].ingress == "INGRESS_TRAFFIC_INTERNAL_ONLY"
    error_message = "the render service takes internal traffic from its invokers only"
  }
  assert {
    condition     = toset([for m in google_cloud_run_v2_service_iam_member.render_invoker : m.member]) == toset(["serviceAccount:run-api@fyniq-staging.iam.gserviceaccount.com", "serviceAccount:run-worker@fyniq-staging.iam.gserviceaccount.com"])
    error_message = "only the api and the worker invoke the render service"
  }
  assert {
    condition     = length([for k, m in google_cloud_run_v2_service_iam_member.invoker : k if m.name == "render"]) == 0
    error_message = "no task invoker calls the render service"
  }
}

# CMEK on the database and the secrets, backups with point-in-time recovery, no public IP, and every
# secret kept in Singapore (CTL-DP-01, SEC-12, NFR-DR-01).
run "the_data_is_encrypted_and_recoverable" {
  command = plan
  variables { spec = run.spec.spec }
  assert {
    condition     = google_sql_database_instance.main.encryption_key_name != null && google_sql_database_instance.main.database_version == "POSTGRES_18"
    error_message = "Cloud SQL for PostgreSQL 18 with a customer-managed key"
  }
  assert {
    condition     = google_sql_database_instance.main.settings[0].backup_configuration[0].enabled && google_sql_database_instance.main.settings[0].backup_configuration[0].point_in_time_recovery_enabled
    error_message = "backups and point-in-time recovery"
  }
  assert {
    condition     = google_sql_database_instance.main.settings[0].backup_configuration[0].location == "asia-southeast1" && google_sql_database_instance.main.settings[0].backup_configuration[0].transaction_log_retention_days == 7
    error_message = "backups pinned to Singapore, with 7 days of point-in-time recovery"
  }
  assert {
    condition     = !google_sql_database_instance.main.settings[0].ip_configuration[0].ipv4_enabled && google_sql_database_instance.main.settings[0].ip_configuration[0].ssl_mode == "ENCRYPTED_ONLY"
    error_message = "a private IP and encrypted connections only"
  }
  assert {
    condition     = google_sql_database_instance.main.deletion_protection && google_sql_database_instance.main.settings[0].availability_type == "ZONAL"
    error_message = "deletion protection; zonal by the cost decision"
  }
  assert {
    condition     = contains([for f in google_sql_database_instance.main.settings[0].database_flags : "${f.name}=${f.value}"], "pgaudit.log=ddl,role")
    error_message = "pgAudit records schema and grant changes, not statements that could carry personal data (SEC-15)"
  }
  assert {
    condition     = alltrue([for s in google_secret_manager_secret.all : s.replication[0].user_managed[0].replicas[0].location == "asia-southeast1" && s.replication[0].user_managed[0].replicas[0].customer_managed_encryption[0].kms_key_name != ""])
    error_message = "every secret stays in Singapore under the customer-managed key"
  }
  assert {
    condition     = alltrue([for k in google_kms_crypto_key.cmek : k.rotation_period == "7776000s"])
    error_message = "the keys rotate every 90 days"
  }
}

# No human holds a standing role: every grant here is to a service account (or allUsers for the public
# services), and elevation goes through Privileged Access Manager with another person's approval.
run "no_standing_human_access" {
  command = plan
  variables { spec = run.spec.spec }
  assert {
    condition = alltrue([for m in concat(
      [for x in google_secret_manager_secret_iam_member.access : x.member],
      [for x in google_cloud_tasks_queue_iam_member.enqueuer : x.member],
      [for x in google_service_account_iam_member.act_as_invoker : x.member],
      [for x in google_cloud_run_v2_service_iam_member.invoker : x.member],
      [for x in google_cloud_run_v2_job_iam_member.run_jobs : x.member],
    ) : startswith(m, "serviceAccount:")])
    error_message = "a grant to a person or a group"
  }
  assert {
    condition     = google_privileged_access_manager_entitlement.break_glass[0].approval_workflow[0].manual_approvals[0].steps[0].approvals_needed == 1 && google_privileged_access_manager_entitlement.break_glass[0].max_request_duration == "3600s"
    error_message = "break-glass needs another person's approval and ends by itself (CTL-IAM-03)"
  }
}

# The installation connectors' secrets come from their manifests (FR-INT-20, PRD-001 rev 0.139): each its own secret,
# read only by the services its manifest lists; the api may add, disable and destroy their versions and no other
# secret's; and a key from before the connector framework is referenced where it is, never copied.
run "connector_secrets_follow_their_manifests" {
  command = plan
  variables {
    spec                     = run.spec.spec
    connector_secret_sources = { "zeptomail.send_token" = "zeptomail-key" }
  }
  assert {
    condition     = contains(keys(google_secret_manager_secret.all), "connector-zeptomail-webhook_key") && contains(keys(google_secret_manager_secret.all), "connector-resend-api_key") && contains(keys(google_secret_manager_secret.all), "zeptomail-key") && !contains(keys(google_secret_manager_secret.all), "connector-zeptomail-send_token")
    error_message = "each installation connector secret is a secret of its own, or the existing one named for it"
  }
  assert {
    condition     = contains(keys(google_secret_manager_secret_iam_member.access), "zeptomail-key/worker") && !contains(keys(google_secret_manager_secret_iam_member.access), "zeptomail-key/api") && contains(keys(google_secret_manager_secret_iam_member.access), "connector-zeptomail-webhook_key/api") && !contains(keys(google_secret_manager_secret_iam_member.access), "connector-zeptomail-webhook_key/worker")
    error_message = "a connector secret is read only by the services its manifest lists (the send token by the worker, the webhook key by the api)"
  }
  assert {
    condition     = toset(keys(google_secret_manager_secret_iam_member.connector_versions)) == toset(values(local.connector_secret_names)) && alltrue([for g in google_secret_manager_secret_iam_member.connector_versions : g.member == "serviceAccount:run-api@fyniq-staging.iam.gserviceaccount.com" && g.role == "roles/secretmanager.secretVersionManager"])
    error_message = "only the api manages versions, of the connectors' secrets only, and the role cannot read a value"
  }
  assert {
    condition     = !contains(keys(google_secret_manager_secret_iam_member.connector_versions), "api-database-url") && !contains(keys(google_secret_manager_secret_iam_member.connector_versions), "idp-api-key")
    error_message = "the api must not add versions to any other secret"
  }
  assert {
    condition     = jsondecode(local.env["worker"]["FYNIQ_CONNECTOR_SECRETS"])["zeptomail.send_token"] == "zeptomail-key" && jsondecode(local.env["api"]["FYNIQ_CONNECTOR_SECRETS"])["resend.webhook_secret"] == "connector-resend-webhook_secret" && !contains(keys(local.env["mcp"]), "FYNIQ_CONNECTOR_SECRETS")
    error_message = "the api, worker and jobs learn which secret holds each connector secret (names only); the MCP server does not"
  }
}
