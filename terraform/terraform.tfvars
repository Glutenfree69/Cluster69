# ============================================================
# KubeQuest - Terraform Variables
# ============================================================
# Copy this file to terraform.tfvars and fill in your values:
#   cp terraform.tfvars.example terraform.tfvars
#
# REQUIRED: Find your public IP with:
#   curl -s ifconfig.me
# ============================================================

# --- Optional: SSH Key ---
# Path to your SSH public key (default: ~/.ssh/id_ed25519.pub)
# ssh_public_key_path = "~/.ssh/id_ed25519.pub"

# --- Optional: Region & Network ---
# aws_region         = "eu-west-3"
# availability_zone  = "eu-west-3a"
# vpc_cidr           = "10.10.0.0/16"
# public_subnet_cidr = "10.10.1.0/24"

# --- Optional: Instance Types ---
# control_plane_instance_type = "t3.medium"
# worker_instance_type        = "t3.small"
# ingress_instance_type       = "t3.small"
# monitoring_instance_type    = "t3.medium"

# --- Optional: Storage ---
# root_volume_size_gb = 20
# root_volume_type    = "gp3"

# --- Optional: Kubernetes ---
# pod_network_cidr = "192.168.0.0/16"
# ubuntu_version   = "jammy"
