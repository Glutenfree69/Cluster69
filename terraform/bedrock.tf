# --- IAM Policy for Bedrock access (shared) ---

resource "aws_iam_policy" "bedrock" {
  name        = "${var.project_name}-bedrock"
  description = "Allow AI agents to invoke models on Amazon Bedrock"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "bedrock:InvokeModel",
          "bedrock:InvokeModelWithResponseStream"
        ]
        Resource = [
          "arn:aws:bedrock:*::foundation-model/anthropic.*",
          "arn:aws:bedrock:*:*:inference-profile/eu.anthropic.*"
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "aws-marketplace:ViewSubscriptions",
          "aws-marketplace:Subscribe"
        ]
        Resource = "*"
      }
    ]
  })
}

# --- IAM User for kagent Bedrock access ---

resource "aws_iam_user" "kagent" {
  name = "${var.project_name}-kagent"

  tags = {
    Name = "${var.project_name}-kagent"
  }
}

resource "aws_iam_access_key" "kagent" {
  user = aws_iam_user.kagent.name
}

resource "aws_iam_policy_attachment" "kagent_bedrock" {
  name       = "${var.project_name}-kagent-bedrock"
  users      = [aws_iam_user.kagent.name]
  policy_arn = aws_iam_policy.bedrock.arn
}

# --- Outputs (retrieve with: terraform output -raw kagent_secret_access_key) ---

output "kagent_access_key_id" {
  description = "AWS Access Key ID for kagent"
  value       = aws_iam_access_key.kagent.id
  sensitive   = true
}

output "kagent_secret_access_key" {
  description = "AWS Secret Access Key for kagent"
  value       = aws_iam_access_key.kagent.secret
  sensitive   = true
}
