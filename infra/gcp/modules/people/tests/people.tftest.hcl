# req: NM-NFR-01
# req: NM-ARC-06

mock_provider "google" {
  mock_resource "google_service_account" {
    defaults = { email = "nemura-x@fyniq-prod.iam.gserviceaccount.com", name = "projects/fyniq-prod/serviceAccounts/nemura-x@fyniq-prod.iam.gserviceaccount.com" }
  }
}

variables {
  project       = "fyniq-prod"
  environment   = "production"
  sql_instance  = "fyniq-production"
  network       = "projects/fyniq-prod/global/networks/platform"
  subnetwork    = "projects/fyniq-prod/regions/asia-southeast1/subnetworks/run"
  public_origin = "https://people.example.com"
  rp_id         = "example.com"
}

# Singapore, scale to zero, every resource labelled app=nemura for the cost split, one image run as the api and as the worker with its role
# in the environment, and the service's ingress as the api (the edge's) and the worker (internal only).
run "nemura_runs_in_singapore_scales_to_zero_and_is_labelled" {
  command = plan
  assert {
    condition = alltrue([for k, s in google_cloud_run_v2_service.services : (
      s.location == "asia-southeast1" && s.labels["app"] == "nemura" && s.template[0].labels["app"] == "nemura" &&
    s.template[0].scaling[0].min_instance_count == 0)])
    error_message = "every Nemura service is in Singapore, at min instances 0, labelled app=nemura"
  }
  assert {
    condition = (google_cloud_tasks_queue.worker.location == "asia-southeast1" && google_sql_database.nemura.name == "nemura" &&
    google_sql_database.nemura.instance == "fyniq-production")
    error_message = "the queue is in Singapore and the database `nemura` is on the shared instance"
  }
  assert {
    condition = (google_cloud_run_v2_service.services["api"].ingress == "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER" &&
    google_cloud_run_v2_service.services["worker"].ingress == "INGRESS_TRAFFIC_INTERNAL_ONLY")
    error_message = "the api takes traffic from the edge only, the worker from inside only"
  }
}

# One image, the role chosen at start: each service gets its own role in NEMURA_ROLE, its own service account and its own database
# user, never another's (NM-ARC-06).
run "each_service_has_its_own_role_identity_and_database_user" {
  command = plan
  assert {
    condition = (one([for e in google_cloud_run_v2_service.services["api"].template[0].containers[0].env : e.value if e.name == "NEMURA_ROLE"]) == "api" &&
    one([for e in google_cloud_run_v2_service.services["worker"].template[0].containers[0].env : e.value if e.name == "NEMURA_ROLE"]) == "worker")
    error_message = "each service runs the role it is: NEMURA_ROLE=api, NEMURA_ROLE=worker"
  }
  assert {
    condition = (google_service_account.services["api"].account_id == "nemura-api" && google_service_account.services["worker"].account_id == "nemura-worker" &&
      google_service_account.services["api"].account_id != google_service_account.services["worker"].account_id &&
    toset([for k, s in google_cloud_run_v2_service.services : s.template[0].service_account]) != toset([]))
    error_message = "one service account per service, named for it"
  }
  assert {
    condition     = alltrue([for k, u in google_sql_user.services : u.type == "CLOUD_IAM_SERVICE_ACCOUNT" && u.instance == "fyniq-production"])
    error_message = "each service connects as an IAM database user on the shared instance: no password anywhere (SEC-12)"
  }
}

# O2: no Nemura identity holds a role on a Fyniq resource. Every IAM binding in the module is on a Nemura resource, except the Cloud SQL
# client rights, narrowed by a condition to the shared instance. Nothing is granted to a person, and nothing to a Fyniq service.
run "no_nemura_identity_holds_a_role_on_a_fyniq_resource" {
  command = plan
  assert {
    condition = alltrue(concat(
      [for k, b in google_project_iam_member.sql_client : b.role == "roles/cloudsql.client" && strcontains(b.condition[0].expression, "instances/fyniq-production")],
      [for k, b in google_project_iam_member.sql_instance_user : b.role == "roles/cloudsql.instanceUser" && strcontains(b.condition[0].expression, "instances/fyniq-production")],
    ))
    error_message = "the project-level rights are the Cloud SQL client rights, each narrowed to the shared instance"
  }
  assert {
    condition = (google_cloud_tasks_queue_iam_member.enqueuer.name == "nemura-worker" && google_cloud_run_v2_service_iam_member.worker_invoker.name == "nemura-worker" &&
      startswith(google_cloud_tasks_queue_iam_member.enqueuer.member, "serviceAccount:nemura-") &&
    startswith(google_cloud_run_v2_service_iam_member.worker_invoker.member, "serviceAccount:nemura-"))
    error_message = "the queue and the worker's invoker are Nemura's own, and no Fyniq account holds a right on them"
  }
  assert {
    condition = alltrue(concat(
      [for k, b in google_project_iam_member.sql_client : startswith(b.member, "serviceAccount:")],
      [for k, b in google_project_iam_member.sql_instance_user : startswith(b.member, "serviceAccount:")],
    ))
    error_message = "nothing is granted to a person"
  }
}

# The hostname is configuration, and a region outside Singapore is refused.
run "a_region_outside_singapore_is_refused" {
  command = plan
  variables {
    region = "us-central1"
  }
  expect_failures = [var.region]
}

run "the_hostname_reaches_the_services_as_configuration" {
  command = plan
  assert {
    condition = (one([for e in google_cloud_run_v2_service.services["api"].template[0].containers[0].env : e.value if e.name == "NEMURA_PUBLIC_ORIGIN"]) == "https://people.example.com" &&
    one([for e in google_cloud_run_v2_service.services["api"].template[0].containers[0].env : e.value if e.name == "NEMURA_RP_ID"]) == "example.com")
    error_message = "the origin and the passkeys' relying-party ID come from variables"
  }
}
