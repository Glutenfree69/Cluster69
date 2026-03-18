# --- IAM User for K8sGPT Bedrock access ---

resource "aws_iam_user" "k8sgpt" {
  name = "${var.project_name}-k8sgpt"

  tags = {
    Name = "${var.project_name}-k8sgpt"
  }
}

resource "aws_iam_access_key" "k8sgpt" {
  user = aws_iam_user.k8sgpt.name
}

resource "aws_iam_policy" "k8sgpt_bedrock" {
  name        = "${var.project_name}-k8sgpt-bedrock"
  description = "Allow K8sGPT to invoke models on Amazon Bedrock"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "bedrock:InvokeModel",
          "bedrock:InvokeModelWithResponseStream"
        ]
        Resource = "arn:aws:bedrock:*::foundation-model/anthropic.*"
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

resource "aws_iam_policy_attachment" "k8sgpt_bedrock" {
  name       = "${var.project_name}-k8sgpt-bedrock"
  users      = [aws_iam_user.k8sgpt.name]
  policy_arn = aws_iam_policy.k8sgpt_bedrock.arn
}

# --- Outputs (retrieve with: terraform output -raw k8sgpt_secret_access_key) ---

output "k8sgpt_access_key_id" {
  description = "AWS Access Key ID for K8sGPT"
  value       = aws_iam_access_key.k8sgpt.id
  sensitive   = true
}

output "k8sgpt_secret_access_key" {
  description = "AWS Secret Access Key for K8sGPT"
  value       = aws_iam_access_key.k8sgpt.secret
  sensitive   = true
}
