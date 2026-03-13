variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "eu-west-3"
}

variable "project_name" {
  description = "Project name used for tagging and naming"
  type        = string
  default     = "kubequest"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.10.0.0/16"
}

variable "public_subnet_cidr" {
  description = "CIDR block for the public subnet"
  type        = string
  default     = "10.10.1.0/24"
}

variable "availability_zone" {
  description = "Availability zone for the subnet"
  type        = string
  default     = "eu-west-3a"
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key for EC2 access"
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "control_plane_instance_type" {
  description = "Instance type for the control plane node"
  type        = string
  default     = "t3.medium"
}

variable "worker_instance_type" {
  description = "Instance type for worker nodes"
  type        = string
  default     = "t3.small"
}

variable "ingress_instance_type" {
  description = "Instance type for the ingress node"
  type        = string
  default     = "t3.small"
}

variable "monitoring_instance_type" {
  description = "Instance type for the monitoring node"
  type        = string
  default     = "t3.medium"
}

variable "root_volume_size_gb" {
  description = "Default root volume size in GB"
  type        = number
  default     = 20
}

variable "root_volume_type" {
  description = "EBS volume type for root volumes"
  type        = string
  default     = "gp3"
}

variable "ubuntu_version" {
  description = "Ubuntu version codename for AMI lookup"
  type        = string
  default     = "jammy"
}

variable "pod_network_cidr" {
  description = "CIDR block for the Kubernetes pod network (Calico default)"
  type        = string
  default     = "192.168.0.0/16"
}
