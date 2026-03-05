# --- SSM Parameter for kubeconfig (value managed by Ansible) ---

resource "aws_ssm_parameter" "kubeconfig" {
  name        = "/${var.project_name}/kubeconfig"
  description = "Kubeconfig for the KubeQuest cluster (updated by Ansible after cluster provisioning)"
  type        = "SecureString"
  tier        = "Advanced"
  value       = "placeholder"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Name = "${var.project_name}-kubeconfig"
  }
}

# --- IAM Policy: read-only access to the kubeconfig parameter ---

resource "aws_iam_policy" "kubeconfig_read" {
  name        = "${var.project_name}-kubeconfig-read"
  description = "Allow reading the KubeQuest kubeconfig from SSM Parameter Store"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ssm:GetParameter"
        ]
        Resource = aws_ssm_parameter.kubeconfig.arn
      }
    ]
  })
}
