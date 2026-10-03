# Step 6: IAM roles for the Lambda functions. Each role allows only the actions
# its function performs, on its own resources.

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

locals {
  table = { for name, t in aws_dynamodb_table.this : name => t.arn }
}

# API Lambda: reads and writes campus data. Accounts live in Cognito, not DynamoDB,
# and the role has no permission to create or delete tables.
resource "aws_iam_role" "api" {
  name               = "${var.project}-api-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

data "aws_iam_policy_document" "api" {
  statement {
    sid       = "WriteOwnLogs"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.api.arn}:*"]
  }

  # Lambda needs these to attach the function to the private subnets.
  statement {
    sid = "VpcNetworkInterfaces"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeSubnets",
      "ec2:DeleteNetworkInterface",
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses",
    ]
    resources = ["*"]
  }

  statement {
    sid       = "Events"
    actions   = ["dynamodb:PutItem", "dynamodb:Query"]
    resources = [local.table["events"], "${local.table["events"]}/index/*"]
  }

  statement {
    sid       = "Rooms"
    actions   = ["dynamodb:Scan", "dynamodb:GetItem", "dynamodb:UpdateItem"]
    resources = [local.table["rooms"]]
  }

  statement {
    sid = "Alerts"
    actions = [
      "dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem",
      "dynamodb:DeleteItem", "dynamodb:ConditionCheckItem", "dynamodb:Query",
    ]
    resources = [local.table["alerts"], "${local.table["alerts"]}/index/*"]
  }

  statement {
    sid       = "ServiceRequests"
    actions   = ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:Query"]
    resources = [local.table["service-requests"], "${local.table["service-requests"]}/index/*"]
  }

  statement {
    sid       = "Aggregates"
    actions   = ["dynamodb:UpdateItem", "dynamodb:Query"]
    resources = [local.table["aggregates"]]
  }

  statement {
    sid       = "Settings"
    actions   = ["dynamodb:GetItem", "dynamodb:PutItem"]
    resources = [local.table["settings"]]
  }
}

resource "aws_iam_role_policy" "api" {
  name   = "campus-data"
  role   = aws_iam_role.api.id
  policy = data.aws_iam_policy_document.api.json
}

