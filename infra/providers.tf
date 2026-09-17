terraform {
  required_version = ">= 1.6"

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
