# The Cloud Run services and jobs (§12.3, ADR-T7): min instances 0, each as its own service account, all
# egress through the private network. Public services take traffic from anywhere and authenticate it
# themselves; the others accept only internal traffic and their invokers.

locals {
  # Plain settings per service: the environment, the wiring, Hydra's admin port, and the root's own.
  env = { for name in keys(local.runners) : name => merge(
    { FYNIQ_ENV = local.spec.environment },
    contains(local.wired, name) ? { FYNIQ_GCP = local.gcp_config } : {},
    contains(local.oauth_admin, name) ? { FYNIQ_OAUTH_ADMIN_URL = local.url["oauth-admin"] } : {},
    lookup(local.default_env, name, {}),
    lookup(var.service_env, name, {}),
  ) }
  health = { for name in keys(local.spec.services) : name => contains(["oauth", "oauth-admin"], name) ? "/health/ready" : "/healthz" }
}

resource "google_cloud_run_v2_service" "services" {
  for_each            = local.spec.services
  project             = var.project
  name                = each.key
  location            = local.region
  ingress             = each.value.public ? "INGRESS_TRAFFIC_ALL" : "INGRESS_TRAFFIC_INTERNAL_ONLY"
  deletion_protection = local.spec.environment == "production"
  template {
    service_account                  = local.email[each.key]
    timeout                          = contains(["worker", "executor"], each.key) ? "1800s" : "300s" # Cloud Tasks waits up to 30 minutes
    max_instance_request_concurrency = 40
    scaling {
      min_instance_count = 0
      max_instance_count = each.value.max_instances
    }
    vpc_access {
      network_interfaces {
        network    = google_compute_network.platform.id
        subnetwork = google_compute_subnetwork.run.id
      }
      egress = "ALL_TRAFFIC"
    }
    containers {
      image = var.placeholder_image
      args  = lookup(local.args, each.key, null)
      ports {
        container_port = each.value.port
      }
      resources {
        limits   = { cpu = "1", memory = "512Mi" }
        cpu_idle = true
      }
      dynamic "env" {
        for_each = local.env[each.key]
        content {
          name  = env.key
          value = env.value
        }
      }
      dynamic "env" {
        for_each = local.secret_env[each.key]
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.all[env.value].secret_id
              version = "latest"
            }
          }
        }
      }
      startup_probe {
        http_get {
          path = local.health[each.key]
        }
      }
      liveness_probe {
        http_get {
          path = local.health[each.key]
        }
      }
    }
  }
  lifecycle {
    ignore_changes = [template[0].containers[0].image, client, client_version] # deployed by digest by the release workflow
  }
  depends_on = [google_service_account.all, google_secret_manager_secret_iam_member.access]
}

resource "google_cloud_run_v2_job" "jobs" {
  for_each            = local.spec.jobs
  project             = var.project
  name                = each.key
  location            = local.region
  deletion_protection = local.spec.environment == "production"
  template {
    task_count = 1
    template {
      service_account = local.email[each.key]
      max_retries     = 1
      timeout         = each.key == "jobs" ? "86400s" : "3600s"
      vpc_access {
        network_interfaces {
          network    = google_compute_network.platform.id
          subnetwork = google_compute_subnetwork.run.id
        }
        egress = "ALL_TRAFFIC"
      }
      containers {
        image = var.placeholder_image
        args  = lookup(local.args, each.key, null)
        resources {
          limits = { cpu = "1", memory = "512Mi" }
        }
        dynamic "env" {
          for_each = local.env[each.key]
          content {
            name  = env.key
            value = env.value
          }
        }
        dynamic "env" {
          for_each = local.secret_env[each.key]
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = google_secret_manager_secret.all[env.value].secret_id
                version = "latest"
              }
            }
          }
        }
      }
    }
  }
  lifecycle {
    ignore_changes = [template[0].template[0].containers[0].image, client, client_version]
  }
  depends_on = [google_service_account.all, google_secret_manager_secret_iam_member.access]
}

# The public services authenticate every request themselves (sessions, OAuth tokens, the tick's OIDC
# token); a load balancer with Cloud Armor fronts them in production from go-live (SEC-23). Hydra's admin
# port takes internal traffic only and has no further check, as in the local stack: the network is its
# boundary [verify in staging that Direct VPC egress counts as internal].
resource "google_cloud_run_v2_service_iam_member" "public" {
  for_each = { for name, s in local.spec.services : name => s if s.public || s.open_inside }
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_service.services[each.key].name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Cloud Run pulls the release images from the CI project's Artifact Registry.
resource "google_artifact_registry_repository_iam_member" "pull" {
  count      = var.artifact_registry == null ? 0 : 1
  project    = var.artifact_registry.project
  location   = var.artifact_registry.location
  repository = var.artifact_registry.repository
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:service-${local.number}@serverless-robot-prod.iam.gserviceaccount.com"
}

# The private render service (§12.3, rev 0.96) is invoked by the api and the worker only, as their own accounts.
resource "google_cloud_run_v2_service_iam_member" "render_invoker" {
  for_each = toset(["api", "worker"])
  project  = var.project
  location = local.region
  name     = google_cloud_run_v2_service.services["render"].name
  role     = "roles/run.invoker"
  member   = local.member[each.key]
}
