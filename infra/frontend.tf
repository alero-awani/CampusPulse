# Dashboard hosting: a private S3 bucket served over HTTPS by CloudFront.
# Only CloudFront can read the bucket (Origin Access Control); it is never public.
# Upload the built dashboard with ./deploy-dashboard.sh.

resource "aws_s3_bucket" "dashboard" {
  bucket        = "${var.project}-dashboard-${data.aws_caller_identity.current.account_id}"
  force_destroy = true # the files are build output and can be re-uploaded
}

resource "aws_s3_bucket_public_access_block" "dashboard" {
  bucket                  = aws_s3_bucket.dashboard.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_cloudfront_origin_access_control" "dashboard" {
  name                              = "${var.project}-dashboard"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_distribution" "dashboard" {
  enabled             = true
  comment             = "CampusPulse dashboard"
  default_root_object = "index.html"
  price_class         = "PriceClass_100" # edge locations in North America and Europe only (cheapest)

  origin {
    origin_id                = "dashboard-bucket"
    domain_name              = aws_s3_bucket.dashboard.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.dashboard.id
  }

  default_cache_behavior {
    target_origin_id           = "dashboard-bucket"
    viewer_protocol_policy     = "redirect-to-https"
    allowed_methods            = ["GET", "HEAD"]
    cached_methods             = ["GET", "HEAD"]
    compress                   = true
    cache_policy_id            = "658327ea-f89d-4fab-a63d-7e88639e58f6" # AWS managed: CachingOptimized
    response_headers_policy_id = "67f7725c-6f97-4210-82d7-5512b31e9d03" # AWS managed: SecurityHeadersPolicy
  }

  # The dashboard is a single-page app: paths like /operations or /callback aren't
  # files in the bucket, so serve the app shell and let the browser route them.
  custom_error_response {
    error_code            = 403
    response_code         = 200
    response_page_path    = "/index.html"
    error_caching_min_ttl = 0
  }

  custom_error_response {
    error_code            = 404
    response_code         = 200
    response_page_path    = "/index.html"
    error_caching_min_ttl = 0
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}

# Allow this CloudFront distribution, and nothing else, to read the files.
data "aws_iam_policy_document" "dashboard_bucket" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.dashboard.arn}/*"]
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

resource "aws_s3_bucket_policy" "dashboard" {
  bucket     = aws_s3_bucket.dashboard.id
  policy     = data.aws_iam_policy_document.dashboard_bucket.json
  depends_on = [aws_s3_bucket_public_access_block.dashboard]
}

locals {
  # Where the dashboard runs: local development addresses plus the CloudFront site.
  dashboard_origins = concat(var.cors_allowed_origins, ["https://${aws_cloudfront_distribution.dashboard.domain_name}"])
}
