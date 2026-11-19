# Nemura, the optional people platform, in one environment (PRD-002 ADR-N1, NM-ARC-06, NM-NFR-01; p1-nemura-platform design D15, the owner's
# decisions of 19 Nov 2026) [live-verify]: written and tested offline here, planned and applied only at the cloud gate.
#   - O1: a second database, `nemura`, on Fyniq's Cloud SQL instance, with roles of its own (nemura_*, from Nemura's migrations),
#     until Nemura holds a first paid person's pay data; then its own instance, a change of this module's `sql_instance` and a
#     restore into it.
#   - O2: Fyniq's project, with Nemura's own services, service accounts, database and queue, every resource labelled app=nemura for the
#     cost split, and no identity here that holds a role on a Fyniq resource (the tests assert it), except the Cloud SQL client right
#     on the shared instance that connecting needs, narrowed by a condition to that instance.
#   - O3: the hostname is configuration (public_origin, rp_id), never code.
# One image, several services (NM-ARC-06): the same `nemura` image runs as the api and the worker, the role in NEMURA_ROLE, each as its
# own service account with its own database role. Min instances 0 (NM-NFR-01: scale to zero). The wiring into the environment module
# (the edge's route to the api, the tick, the issuer) joins when Nemura is deployed, at the cloud gate.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The environment's own project (fyniq-staging, fyniq-prod): Nemura shares it (O2)."
  type        = string
}

variable "environment" {
  description = "staging or production."
  type        = string
  validation {
    condition     = contains(["staging", "production"], var.environment)
    error_message = "environment must be staging or production."
  }
}

variable "region" {
  description = "Singapore (NM-NFR-01, CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
  validation {
    condition     = var.region == "asia-southeast1"
    error_message = "Nemura runs in asia-southeast1 only (NM-NFR-01, CTL-DP-07)."
  }
}

variable "sql_instance" {
  description = "Fyniq's Cloud SQL instance, which holds Nemura's database too until the first paid person's pay data (O1)."
  type        = string
}

variable "network" {
  description = "The environment's private network (its id), as the other services use."
  type        = string
}

variable "subnetwork" {
  description = "The subnetwork Cloud Run's private egress uses (its id)."
  type        = string
}

variable "public_origin" {
  description = "Nemura's https origin, people.<domain> (O3): where people sign in, and the base of its API and issuer URLs."
  type        = string
  validation {
    condition     = can(regex("^https://[a-z0-9.-]+$", var.public_origin))
    error_message = "public_origin must be an https origin with no path, such as https://people.example.com."
  }
}

variable "rp_id" {
  description = "The passkeys' relying-party ID: the registrable domain Fyniq and Nemura share (question 10), so one passkey works in both."
  type        = string
  validation {
    condition     = can(regex("^[a-z0-9.-]+$", var.rp_id)) && !strcontains(var.rp_id, "/")
    error_message = "rp_id must be a registrable domain, such as example.com."
  }
}

variable "placeholder_image" {
  description = "The image a service starts from, before the release workflow deploys the nemura image by digest."
  type        = string
  default     = "us-docker.pkg.dev/cloudrun/container/hello"
}

variable "max_instances" {
  description = "The most instances each service may scale to."
  type        = number
  default     = 3
}

locals {
  labels = { app = "nemura", environment = var.environment }
  # One service per role, each its own identity and database role (NM-ARC-06). The executor and the assistant arrive with the changes
  # that build them. Public: the api takes traffic from the edge's load balancer; the worker accepts only its queue's invoker.
  services = {
    api    = { public = true, role = "api", db_role = "nemura_api" }
    worker = { public = false, role = "worker", db_role = "nemura_worker" }
  }
}

resource "google_service_account" "services" {
  for_each     = local.services
  project      = var.project
  account_id   = "nemura-${each.key}"
  display_name = "Nemura ${each.key} (${var.environment})"
}

# The queue's own invoker, so only Cloud Tasks reaches the worker (ADR-T7).
resource "google_service_account" "invoker" {
  project      = var.project
  account_id   = "nemura-tasks-worker"
  display_name = "Nemura task invoker (${var.environment})"
}

# Nemura's database on the shared instance (O1) and one IAM database user per service: each is a member of its own group role
# (nemura_api, nemura_worker), which Nemura's migrations create and grant; no service connects as the migrator.
resource "google_sql_database" "nemura" {
  project  = var.project
  instance = var.sql_instance
  name     = "nemura"
}

resource "google_sql_user" "services" {
  for_each = local.services
  project  = var.project
  instance = var.sql_instance
  # An IAM service account's database user is its email without the suffix.
  name = trimsuffix(google_service_account.services[each.key].email, ".gserviceaccount.com")
  type = "CLOUD_IAM_SERVICE_ACCOUNT"
}

# Connecting to Cloud SQL as an IAM user needs the client and instance-user roles on the project: narrowed to the shared instance, which
# is the one Fyniq resource a Nemura identity may touch (O2).
resource "google_project_iam_member" "sql_client" {
  for_each = local.services
  project  = var.project
  role     = "roles/cloudsql.client"
  member   = "serviceAccount:${google_service_account.services[each.key].email}"
  condition {
    title       = "nemura-${each.key} on the shared instance only"
    description = "The shared Cloud SQL instance, never another resource."
    expression  = "resource.name == \"projects/${var.project}/instances/${var.sql_instance}\""
  }
}

resource "google_project_iam_member" "sql_instance_user" {
  for_each = local.services
  project  = var.project
  role     = "roles/cloudsql.instanceUser"
  member   = "serviceAccount:${google_service_account.services[each.key].email}"
  condition {
    title       = "nemura-${each.key} on the shared instance only"
    description = "The shared Cloud SQL instance, never another resource."
    expression  = "resource.name == \"projects/${var.project}/instances/${var.sql_instance}\""
  }
}

resource "google_cloud_run_v2_service" "services" {
  for_each = local.services
  project  = var.project
  name     = "nemura-${each.key}"
  location = var.region
  labels   = local.labels
  # The api takes traffic from the edge's load balancer and from inside Google Cloud only (SEC-23); the worker, internal traffic only.
  ingress             = each.value.public ? "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER" : "INGRESS_TRAFFIC_INTERNAL_ONLY"
  deletion_protection = var.environment == "production"
  # Binary Authorization's project policy admits only images the release workflow attested (SEC-22).
  binary_authorization {
    use_default = true
  }
  template {
    service_account                  = google_service_account.services[each.key].email
    timeout                          = each.key == "worker" ? "1800s" : "300s"
    max_instance_request_concurrency = 40
    labels                           = local.labels
    scaling {
      min_instance_count = 0 # scale to zero (NM-NFR-01, ADR-T7)
      max_instance_count = var.max_instances
    }
    vpc_access {
      network_interfaces {
        network    = var.network
        subnetwork = var.subnetwork
      }
      egress = "ALL_TRAFFIC"
    }
    containers {
      name  = "nemura"
      image = var.placeholder_image
      ports {
        container_port = 8080
      }
      resources {
        limits   = { cpu = "1", memory = "512Mi" }
        cpu_idle = true
      }
      env {
        name  = "FYNIQ_ENV"
        value = var.environment
      }
      env {
        name  = "NEMURA_ROLE"
        value = each.value.role
      }
      env {
        name  = "NEMURA_PUBLIC_ORIGIN"
        value = var.public_origin
      }
      env {
        name  = "NEMURA_RP_ID"
        value = var.rp_id
      }
      startup_probe {
        http_get {
          path = "/readyz"
        }
      }
      liveness_probe {
        http_get {
          path = "/healthz"
        }
      }
    }
  }
  lifecycle {
    ignore_changes = [template[0].containers[0].image, client, client_version] # deployed by digest by the release workflow
  }
}

# One Cloud Tasks queue for the worker class, delivering to the worker with the invoker's own token; only the api may enqueue.
resource "google_cloud_tasks_queue" "worker" {
  project  = var.project
  name     = "nemura-worker"
  location = var.region
  rate_limits {
    max_concurrent_dispatches = 10
    max_dispatches_per_second = 5
  }
  retry_config {
    max_attempts  = 5 # the row's own attempts decide when it is dead (internal/nemura/outbox)
    min_backoff   = "10s"
    max_backoff   = "300s"
    max_doublings = 4
  }
  stackdriver_logging_config {
    sampling_ratio = 1.0
  }
}

resource "google_cloud_tasks_queue_iam_member" "enqueuer" {
  project  = var.project
  location = var.region
  name     = google_cloud_tasks_queue.worker.name
  role     = "roles/cloudtasks.enqueuer"
  member   = "serviceAccount:${google_service_account.services["api"].email}"
}

resource "google_service_account_iam_member" "api_acts_as_invoker" {
  service_account_id = google_service_account.invoker.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.services["api"].email}"
}

resource "google_cloud_run_v2_service_iam_member" "worker_invoker" {
  project  = var.project
  location = var.region
  name     = google_cloud_run_v2_service.services["worker"].name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.invoker.email}"
}

output "services" {
  description = "Nemura's services: name, role and the service account each runs as."
  value = { for k, s in local.services : k => {
    name            = google_cloud_run_v2_service.services[k].name
    role            = s.role
    service_account = google_service_account.services[k].email
    database_role   = s.db_role
  } }
}

output "database" {
  description = "Nemura's database and the instance it shares (O1)."
  value       = { name = google_sql_database.nemura.name, instance = var.sql_instance }
}
