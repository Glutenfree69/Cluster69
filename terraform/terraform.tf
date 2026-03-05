terraform {
  required_version = ">= 1.14.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    bucket = "logs69"
    key    = "kubequest/terraform.tfstate"
    region = "eu-west-3"
  }
}
