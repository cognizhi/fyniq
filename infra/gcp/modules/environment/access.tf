# No human holds a standing role in this project (CTL-IAM-03, SEC-03): an elevation is requested through
# Privileged Access Manager, approved by another person, time-limited and logged.

resource "google_privileged_access_manager_entitlement" "break_glass" {
  count                = var.break_glass == null ? 0 : 1
  entitlement_id       = "break-glass"
  location             = "global"
  parent               = "projects/${var.project}"
  max_request_duration = var.break_glass.duration
  eligible_users {
    principals = var.break_glass.requesters
  }
  privileged_access {
    gcp_iam_access {
      resource      = "//cloudresourcemanager.googleapis.com/projects/${var.project}"
      resource_type = "cloudresourcemanager.googleapis.com/Project"
      role_bindings {
        role = var.break_glass.role
      }
    }
  }
  requester_justification_config {
    unstructured {}
  }
  approval_workflow {
    manual_approvals {
      require_approver_justification = true
      steps {
        approvals_needed = 1
        approvers {
          principals = var.break_glass.approvers
        }
      }
    }
  }
  depends_on = [google_project_service.apis]
}
