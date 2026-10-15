# The platform as cloud-neutral data (NFR-PORT-05, ADR-T8): the services and jobs of §12.3, the worker
# classes of §12.11 and the sizes, schedules, budgets and retention an environment chooses. A cloud's
# modules (infra/gcp/, later AWS and Azure) implement this spec; none of it names a cloud resource.

terraform {
  required_version = ">= 1.13.0"
}

variable "environment" {
  description = "staging or production. The dev project is a sandbox without the platform: the release build runs only in staging and production (RUNTIME-FLAVOUR-01)."
  type        = string
  validation {
    condition     = contains(["staging", "production"], var.environment)
    error_message = "environment must be staging or production."
  }
}

variable "region" {
  description = "Where every resource lives (CTL-DP-07: Singapore only, except documented transfers, of which there are none)."
  type        = string
  default     = "asia-southeast1"
  validation {
    condition     = var.region == "asia-southeast1"
    error_message = "region must be asia-southeast1: resources stay in Singapore (CTL-DP-07, PDPA transfer limitation)."
  }
}

variable "max_instances" {
  description = "The most instances a service may scale to, by service. Every service scales to zero (ADR-T7)."
  type        = map(number)
  default     = {}
  validation {
    condition     = alltrue([for n in values(var.max_instances) : n >= 1 && n <= 100])
    error_message = "max_instances must be between 1 and 100."
  }
}

variable "tick_minutes" {
  description = "How often the Cloud Scheduler tick wakes the API (§12.11: every 5 minutes by default; a setting)."
  type        = number
  default     = 5
  validation {
    condition     = contains([1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30], var.tick_minutes)
    error_message = "tick_minutes must divide an hour evenly (1, 2, 3, 4, 5, 6, 10, 12, 15, 20 or 30)."
  }
}

variable "database" {
  description = "The database instance (§12.6): tier and availability. Zonal at first, by the cost decision (rev 0.19); HA later (NFR-COST-04)."
  type = object({
    tier         = string
    availability = string
    disk_gb      = number
  })
  default = { tier = "db-custom-1-3840", availability = "ZONAL", disk_gb = 10 }
  validation {
    condition     = contains(["ZONAL", "REGIONAL"], var.database.availability) && var.database.disk_gb >= 10
    error_message = "database.availability must be ZONAL or REGIONAL, and disk_gb at least 10."
  }
}

variable "budget" {
  description = "The monthly budget in US dollars and who is told (open question 9, rev 0.20): alerts at 50%, 80% and 100%, never a cap."
  type = object({
    amount_usd = number
    emails     = list(string)
  })
  validation {
    condition     = var.budget.amount_usd > 0 && length(var.budget.emails) > 0
    error_message = "budget.amount_usd must be positive, and budget.emails name at least one person."
  }
}

variable "anchors" {
  description = "The audit anchors' bucket (CTL-AUD-02): how long an anchor is kept, and whether the retention is locked. A lock is irreversible: true only in production."
  type = object({
    retention_days = number
    lock           = bool
  })
  validation {
    condition     = var.anchors.retention_days >= 1
    error_message = "anchors.retention_days must be at least 1."
  }
}

locals {
  # §12.3 and SEC-03: one identity per service and job. database_role is the PostgreSQL role the
  # migrations create for it (db/migrations); null means no database access at all.
  services = {
    api = { image = "api", port = 8080, public = true, open_inside = false, database_role = "fyniq_api", class = null }
    ai  = { image = "ai", port = 8080, public = true, open_inside = false, database_role = null, class = null }  # reads and proposes through the API only
    mcp = { image = "mcp", port = 8080, public = true, open_inside = false, database_role = null, class = null } # no database access at all (§12.3)
    # The worker serves the worker class, and the jobs class as local mode does (internal/wiring/local.go);
    # the long-work handler that starts Cloud Run jobs is P1.
    worker   = { image = "worker", port = 8080, public = false, open_inside = false, database_role = "fyniq_worker", class = "worker" }
    executor = { image = "executor", port = 8080, public = false, open_inside = false, database_role = "fyniq_executor", class = "executor" }
    render   = { image = "render", port = 8080, public = false, open_inside = false, database_role = null, class = null }
    # Ory Hydra (ADR-T3): its public port for clients; its admin port reachable from inside the network
    # only, with no further check, as in the local stack (the api's Hydra client sends no ID token).
    oauth       = { image = "oauth", port = 4444, public = true, open_inside = false, database_role = null, class = null }
    oauth-admin = { image = "oauth", port = 4445, public = false, open_inside = true, database_role = null, class = null }
  }
  jobs = {
    migrate = { image = "migrate", database_role = "fyniq_migrator" }
    jobs    = { image = "jobs", database_role = "fyniq_worker" }
    # Hydra's schema in its own database (ADR-T3), before each Hydra release, as the local stack does.
    oauth-migrate = { image = "oauth", database_role = null }
  }
  # §12.11: one queue per worker class, each with its own invoker identity; the class's handler.
  classes = {
    worker   = { service = "worker" }
    executor = { service = "executor" }
    jobs     = { service = "worker" }
  }
}

output "spec" {
  description = "The platform, cloud-neutral."
  value = {
    environment = var.environment
    region      = var.region
    services = { for name, s in local.services : name => merge(s, {
      max_instances = lookup(var.max_instances, name, var.environment == "production" ? 10 : 3)
    }) }
    jobs         = local.jobs
    classes      = local.classes
    tick_minutes = var.tick_minutes
    tick_cron    = "*/${var.tick_minutes} * * * *"
    database     = var.database
    budget       = merge(var.budget, { thresholds = [0.5, 0.8, 1.0] })
    anchors      = var.anchors
  }
}
