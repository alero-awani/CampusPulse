# Step 7: API Gateway HTTP API, the public entry point.
#
# Public routes: GET /health, POST /auth/login (login Lambda), and the sensor
# routes POST /events and POST /events/batch (the API checks the device key).
# Every other route requires a valid Cognito ID token, checked by API Gateway
# before the request reaches the Lambda.

resource "aws_apigatewayv2_api" "main" {
  name          = "${var.project}-api"
  protocol_type = "HTTP"

  # API Gateway answers browser preflight requests itself, before routes and authorizers.
  cors_configuration {
    allow_origins  = local.dashboard_origins
    allow_methods  = ["GET", "POST", "PUT", "PATCH", "OPTIONS"]
    allow_headers  = ["authorization", "content-type", "x-device-key", "x-request-id"]
    expose_headers = ["x-request-id"]
    max_age        = 600
  }
}

resource "aws_apigatewayv2_authorizer" "cognito" {
  api_id           = aws_apigatewayv2_api.main.id
  name             = "cognito"
  authorizer_type  = "JWT"
  identity_sources = ["$request.header.Authorization"]

  # Accept only tokens issued by our user pool for our app client.
  jwt_configuration {
    issuer   = "https://cognito-idp.${var.region}.amazonaws.com/${aws_cognito_user_pool.main.id}"
    audience = [aws_cognito_user_pool_client.api.id]
  }
}

resource "aws_apigatewayv2_integration" "api" {
  api_id                 = aws_apigatewayv2_api.main.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.api.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_integration" "login" {
  api_id                 = aws_apigatewayv2_api.main.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.login.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "public" {
  # "OPTIONS /{proxy+}" lets browser CORS preflight checks through: browsers never
  # send the Authorization header on them, so the $default route's token check
  # would reject them and block every dashboard request.
  for_each = toset(["GET /health", "POST /events", "POST /events/batch", "OPTIONS /{proxy+}"])

  api_id    = aws_apigatewayv2_api.main.id
  route_key = each.key
  target    = "integrations/${aws_apigatewayv2_integration.api.id}"
}

resource "aws_apigatewayv2_route" "login" {
  api_id    = aws_apigatewayv2_api.main.id
  route_key = "POST /auth/login"
  target    = "integrations/${aws_apigatewayv2_integration.login.id}"
}

# Everything else (dashboard routes) requires a Cognito token.
resource "aws_apigatewayv2_route" "authenticated" {
  api_id             = aws_apigatewayv2_api.main.id
  route_key          = "$default"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

resource "aws_cloudwatch_log_group" "api_access" {
  name              = "/aws/apigateway/${var.project}-api"
  retention_in_days = 7
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.main.id
  name        = "$default"
  auto_deploy = true

  # Rate limits protect against floods and runaway costs.
  default_route_settings {
    throttling_rate_limit  = 20 # requests per second, sustained
    throttling_burst_limit = 50
  }

  # Stricter limit on login to slow down password guessing.
  route_settings {
    route_key              = aws_apigatewayv2_route.login.route_key
    throttling_rate_limit  = 2
    throttling_burst_limit = 5
  }

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_access.arn
    format = jsonencode({
      requestId        = "$context.requestId"
      ip               = "$context.identity.sourceIp"
      time             = "$context.requestTime"
      method           = "$context.httpMethod"
      path             = "$context.path"
      routeKey         = "$context.routeKey"
      status           = "$context.status"
      latencyMs        = "$context.responseLatency"
      authorizerError  = "$context.authorizer.error"
      integrationError = "$context.integrationErrorMessage"
    })
  }
}

# Allow this API, and nothing else, to invoke the Lambda functions.
resource "aws_lambda_permission" "api" {
  statement_id  = "AllowApiGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.main.execution_arn}/*/*"
}

resource "aws_lambda_permission" "login" {
  statement_id  = "AllowApiGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.login.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.main.execution_arn}/*/*"
}
