# Disaster recovery for S3: both buckets are copied to a second region.
#   dashboard bucket  a bad deploy can be rolled back (versioning), and the site
#                     files survive a region outage
#   state bucket      Terraform state is the one thing that cannot be rebuilt
#
# The state bucket itself is created by bootstrap-state.sh (it must exist before
# Terraform runs), but its replication is managed here.

provider "aws" {
  alias  = "replica"
  region = var.replica_region

  default_tags {
    tags = {
      Project   = "CampusPulse"
      ManagedBy = "Terraform"
    }
  }
}

locals {
  state_bucket = "${var.project}-tfstate-${data.aws_caller_identity.current.account_id}"
}

# Versioning is required for replication, and lets a bad dashboard deploy be undone.
resource "aws_s3_bucket_versioning" "dashboard" {
  bucket = aws_s3_bucket.dashboard.id
  versioning_configuration {
    status = "Enabled"
  }
}

# Old versions are deleted after 90 days so storage stays flat.
resource "aws_s3_bucket_lifecycle_configuration" "dashboard" {
  bucket     = aws_s3_bucket.dashboard.id
  depends_on = [aws_s3_bucket_versioning.dashboard]

  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}

# The copies, in the second region.
resource "aws_s3_bucket" "replica" {
  for_each = toset(["dashboard", "tfstate"])
  provider = aws.replica

  bucket        = "${var.project}-${each.key}-${data.aws_caller_identity.current.account_id}-${var.replica_region}"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "replica" {
  for_each = aws_s3_bucket.replica
  provider = aws.replica

  bucket = each.value.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "replica" {
  for_each = aws_s3_bucket.replica
  provider = aws.replica

  bucket                  = each.value.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "replica" {
  for_each   = aws_s3_bucket.replica
  provider   = aws.replica
  depends_on = [aws_s3_bucket_versioning.replica]

  bucket = each.value.id
  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}

# S3 copies objects for us using this role: read on the sources, write on the copies.
data "aws_iam_policy_document" "replication_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["s3.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "replication" {
  name               = "${var.project}-s3-replication"
  assume_role_policy = data.aws_iam_policy_document.replication_assume.json
}

data "aws_iam_policy_document" "replication" {
  statement {
    sid     = "ReadSourceBuckets"
    actions = ["s3:GetReplicationConfiguration", "s3:ListBucket"]
    resources = [
      aws_s3_bucket.dashboard.arn,
      "arn:aws:s3:::${local.state_bucket}",
    ]
  }

  statement {
    sid     = "ReadSourceObjects"
    actions = ["s3:GetObjectVersionForReplication", "s3:GetObjectVersionAcl", "s3:GetObjectVersionTagging"]
    resources = [
      "${aws_s3_bucket.dashboard.arn}/*",
      "arn:aws:s3:::${local.state_bucket}/*",
    ]
  }

  statement {
    sid       = "WriteCopies"
    actions   = ["s3:ReplicateObject", "s3:ReplicateDelete", "s3:ReplicateTags"]
    resources = [for b in aws_s3_bucket.replica : "${b.arn}/*"]
  }
}

resource "aws_iam_role_policy" "replication" {
  name   = "replicate-buckets"
  role   = aws_iam_role.replication.id
  policy = data.aws_iam_policy_document.replication.json
}

resource "aws_s3_bucket_replication_configuration" "dashboard" {
  bucket     = aws_s3_bucket.dashboard.id
  role       = aws_iam_role.replication.arn
  depends_on = [aws_s3_bucket_versioning.dashboard]

  rule {
    id     = "to-replica-region"
    status = "Enabled"
    filter {}
    delete_marker_replication {
      status = "Enabled"
    }
    destination {
      bucket        = aws_s3_bucket.replica["dashboard"].arn
      storage_class = "STANDARD"
    }
  }
}

resource "aws_s3_bucket_replication_configuration" "tfstate" {
  bucket = local.state_bucket
  role   = aws_iam_role.replication.arn

  rule {
    id     = "to-replica-region"
    status = "Enabled"
    filter {}
    delete_marker_replication {
      status = "Enabled"
    }
    destination {
      bucket        = aws_s3_bucket.replica["tfstate"].arn
      storage_class = "STANDARD"
    }
  }
}

# Let the dashboard's CloudFront distribution, and nothing else, read the copy,
# so failover works. Same rule as the primary bucket.
data "aws_iam_policy_document" "dashboard_replica_bucket" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.replica["dashboard"].arn}/*"]
    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.dashboard.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "dashboard_replica" {
  provider   = aws.replica
  bucket     = aws_s3_bucket.replica["dashboard"].id
  policy     = data.aws_iam_policy_document.dashboard_replica_bucket.json
  depends_on = [aws_s3_bucket_public_access_block.replica]
}
