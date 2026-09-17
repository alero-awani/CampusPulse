output "cognito_user_pool_id" {
  value = aws_cognito_user_pool.main.id
}

output "cognito_client_id" {
  value = aws_cognito_user_pool_client.api.id
}

output "device_api_key" {
  value     = random_password.device_key.result
  sensitive = true
}

output "api_url" {
  value = aws_apigatewayv2_api.main.api_endpoint
}

output "cognito_domain" {
  value = "https://${aws_cognito_user_pool_domain.main.domain}.auth.${var.region}.amazoncognito.com"
}

output "dashboard_url" {
  value = "https://${aws_cloudfront_distribution.dashboard.domain_name}"
}

output "dashboard_bucket" {
  value = aws_s3_bucket.dashboard.bucket
}

output "dashboard_distribution_id" {
  value = aws_cloudfront_distribution.dashboard.id
}

output "monitoring_dashboard_url" {
  value = "https://${var.region}.console.aws.amazon.com/cloudwatch/home?region=${var.region}#dashboards/dashboard/${aws_cloudwatch_dashboard.main.dashboard_name}"
}
