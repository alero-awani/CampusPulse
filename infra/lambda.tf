# Step 6: the Go API as two Lambda functions built from the same binary.
#   campuspulse-api    all API routes except login; runs in the private subnets
#   campuspulse-login  POST /auth/login; runs outside the VPC so it can reach Cognito
# Build the binary first with ./build-lambda.sh.

data "archive_file" "backend" {
  type        = "zip"
  source_file = "${path.module}/../backend/dist/lambda/bootstrap"
  output_path = "${path.module}/.build/backend.zip"
}

# Shared key that sensors (the simulator) send in the X-Device-Key header.
resource "random_password" "device_key" {
  length  = 40
  special = false
}

locals {
  lambda_env = {
    APP_ENV           = "aws"
    AUTH_MODE         = "cognito"
    COGNITO_CLIENT_ID = aws_cognito_user_pool_client.api.id
    TABLE_PREFIX      = "${var.project}-"
    CAMPUS_TIMEZONE   = "Europe/Paris"
  }
}

# Log groups are created here, not by Lambda, so logs are deleted after 7 days.
resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/lambda/${var.project}-api"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "login" {
  name              = "/aws/lambda/${var.project}-login"
  retention_in_days = 7
}

resource "aws_lambda_function" "api" {
  function_name    = "${var.project}-api"
  role             = aws_iam_role.api.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = data.archive_file.backend.output_path
  source_code_hash = data.archive_file.backend.output_base64sha256
  memory_size      = 256
  timeout          = 15

  vpc_config {
    subnet_ids         = aws_subnet.private[*].id
    security_group_ids = [aws_security_group.lambda.id]
  }

  # Environment variables are encrypted at rest with the AWS-managed Lambda key.
  environment {
    variables = merge(local.lambda_env, { DEVICE_API_KEYS = random_password.device_key.result })
  }

  depends_on = [aws_cloudwatch_log_group.api, aws_iam_role_policy.api]
}

resource "aws_lambda_function" "login" {
  function_name    = "${var.project}-login"
  role             = aws_iam_role.login.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = data.archive_file.backend.output_path
  source_code_hash = data.archive_file.backend.output_base64sha256
  memory_size      = 128
  timeout          = 10

  environment {
    variables = local.lambda_env
  }

  depends_on = [aws_cloudwatch_log_group.login, aws_iam_role_policy.login]
}
