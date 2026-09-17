terraform {
  required_version = ">= 1.11"

  # State is shared in S3 (created by bootstrap-state.sh): versioned, encrypted,
  # HTTPS only. The lock file stops two people or pipelines applying at once.
  backend "s3" {
    bucket       = "campuspulse-tfstate-278746617511"
    key          = "campuspulse/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "aws" {
  region = var.region

  # Every resource gets these tags, so the project's resources and costs can be
  # filtered in the console and in Cost Explorer.
  default_tags {
    tags = {
      Project   = "CampusPulse"
      ManagedBy = "Terraform"
    }
  }
}
