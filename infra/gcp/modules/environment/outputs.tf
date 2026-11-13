output "accounts" {
  description = "Each service's, job's and invoker's service account."
  value       = local.email
}

output "urls" {
  description = "Each service's URL."
  value       = local.url
}

output "gcp_config" {
  description = "FYNIQ_GCP, as the services read it (internal/wiring/release.go)."
  value       = local.gcp_config
}

output "database" {
  description = "The Cloud SQL instance's connection name."
  value       = google_sql_database_instance.main.connection_name
}

output "database_instance" {
  description = "The Cloud SQL instance's name."
  value       = google_sql_database_instance.main.name
}

output "database_agent" {
  description = "The Cloud SQL instance's own service account, which writes its exports."
  value       = google_sql_database_instance.main.service_account_email_address
}

output "members" {
  description = "Each account as an IAM member, once it exists (for grants made in another module, such as the anchors)."
  value       = { for k, sa in google_service_account.all : k => sa.member }
}

output "org_secrets_project" {
  description = "The project of the organisations' connector secrets (org_secrets.tf)."
  value       = var.secrets_project
}
