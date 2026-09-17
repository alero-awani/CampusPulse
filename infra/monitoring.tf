# Step 9: monitoring. CloudWatch alarms email the team through SNS, and a
# CloudWatch dashboard shows the platform's health on one page.

resource "aws_sns_topic" "alarms" {
  name = "${var.project}-alarms"
}

# AWS sends a confirmation email; alarms are only delivered after the link in it is clicked.
resource "aws_sns_topic_subscription" "alarms_email" {
  topic_arn = aws_sns_topic.alarms.arn
  protocol  = "email"
  endpoint  = var.budget_email
}

locals {
  api_id = aws_apigatewayv2_api.main.id

  alarms = {
    "api-lambda-errors" = {
      description = "The API Lambda failed (crashed or timed out)."
      namespace   = "AWS/Lambda"
      metric      = "Errors"
      dimensions  = { FunctionName = aws_lambda_function.api.function_name }
      statistic   = "Sum"
      threshold   = 1
    }
    "login-lambda-errors" = {
      description = "The login Lambda failed (crashed or timed out)."
      namespace   = "AWS/Lambda"
      metric      = "Errors"
      dimensions  = { FunctionName = aws_lambda_function.login.function_name }
      statistic   = "Sum"
      threshold   = 1
    }
    "api-lambda-throttles" = {
      description = "Lambda refused API requests because too many were running at once."
      namespace   = "AWS/Lambda"
      metric      = "Throttles"
      dimensions  = { FunctionName = aws_lambda_function.api.function_name }
      statistic   = "Sum"
      threshold   = 1
    }
    "api-5xx" = {
      description = "The API returned 5 or more server errors in 5 minutes."
      namespace   = "AWS/ApiGateway"
      metric      = "5xx"
      dimensions  = { ApiId = local.api_id }
      statistic   = "Sum"
      threshold   = 5
    }
    "api-4xx-spike" = {
      description = "50 or more rejected requests in 5 minutes: possible password guessing, bad tokens, or rate limiting."
      namespace   = "AWS/ApiGateway"
      metric      = "4xx"
      dimensions  = { ApiId = local.api_id }
      statistic   = "Sum"
      threshold   = 50
    }
    "ingestion-failures" = {
      description = "Sensor events could not be stored."
      namespace   = "CampusPulse"
      metric      = "EventsFailed"
      dimensions  = { Service = "api" }
      statistic   = "Sum"
      threshold   = 1
    }
    "campus-alerts-raised" = {
      description = "The platform detected a new campus problem (overcrowding, temperature, humidity, equipment, doors, or energy)."
      namespace   = "CampusPulse"
      metric      = "AlertsRaised"
      dimensions  = { Service = "api" }
      statistic   = "Sum"
      threshold   = 1
    }
  }
}

resource "aws_cloudwatch_metric_alarm" "this" {
  for_each = local.alarms

  alarm_name          = "${var.project}-${each.key}"
  alarm_description   = each.value.description
  namespace           = each.value.namespace
  metric_name         = each.value.metric
  dimensions          = each.value.dimensions
  statistic           = each.value.statistic
  period              = 300
  evaluation_periods  = 1
  threshold           = each.value.threshold
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching" # no traffic is not a problem
  alarm_actions       = [aws_sns_topic.alarms.arn]
  ok_actions          = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "api_latency" {
  alarm_name          = "${var.project}-api-latency-p95"
  alarm_description   = "95% of API requests took longer than 3 seconds, for 15 minutes."
  namespace           = "AWS/ApiGateway"
  metric_name         = "Latency"
  dimensions          = { ApiId = local.api_id }
  extended_statistic  = "p95"
  period              = 300
  evaluation_periods  = 3
  threshold           = 3000
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alarms.arn]
  ok_actions          = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_dashboard" "main" {
  dashboard_name = "CampusPulse"

  dashboard_body = jsonencode({
    widgets = [
      {
        type = "alarm", x = 0, y = 0, width = 24, height = 3
        properties = {
          title  = "Alarms"
          alarms = concat([for a in aws_cloudwatch_metric_alarm.this : a.arn], [aws_cloudwatch_metric_alarm.api_latency.arn])
        }
      },
      {
        type = "metric", x = 0, y = 3, width = 8, height = 6
        properties = {
          title  = "API requests and errors"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            ["AWS/ApiGateway", "Count", "ApiId", local.api_id, { label = "Requests" }],
            ["AWS/ApiGateway", "4xx", "ApiId", local.api_id, { label = "4xx (rejected)" }],
            ["AWS/ApiGateway", "5xx", "ApiId", local.api_id, { label = "5xx (server errors)" }],
          ]
        }
      },
      {
        type = "metric", x = 8, y = 3, width = 8, height = 6
        properties = {
          title  = "API latency (ms)"
          region = var.region
          period = 300
          metrics = [
            ["AWS/ApiGateway", "Latency", "ApiId", local.api_id, { stat = "p50", label = "p50" }],
            ["AWS/ApiGateway", "Latency", "ApiId", local.api_id, { stat = "p95", label = "p95" }],
          ]
        }
      },
      {
        type = "metric", x = 16, y = 3, width = 8, height = 6
        properties = {
          title  = "Lambda invocations and errors"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            ["AWS/Lambda", "Invocations", "FunctionName", aws_lambda_function.api.function_name, { label = "API invocations" }],
            ["AWS/Lambda", "Errors", "FunctionName", aws_lambda_function.api.function_name, { label = "API errors" }],
            ["AWS/Lambda", "Invocations", "FunctionName", aws_lambda_function.login.function_name, { label = "Login invocations" }],
            ["AWS/Lambda", "Errors", "FunctionName", aws_lambda_function.login.function_name, { label = "Login errors" }],
          ]
        }
      },
      {
        type = "metric", x = 0, y = 9, width = 8, height = 6
        properties = {
          title  = "Sensor events"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            ["CampusPulse", "EventsIngested", "Service", "api", { label = "Stored" }],
            ["CampusPulse", "EventsDuplicate", "Service", "api", { label = "Duplicates" }],
            ["CampusPulse", "EventsRejected", "Service", "api", { label = "Rejected" }],
            ["CampusPulse", "EventsFailed", "Service", "api", { label = "Failed" }],
          ]
        }
      },
      {
        type = "metric", x = 8, y = 9, width = 8, height = 6
        properties = {
          title  = "Campus activity"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            ["CampusPulse", "AlertsRaised", "Service", "api", { label = "Alerts raised" }],
            ["CampusPulse", "ServiceRequestsCreated", "Service", "api", { label = "Service requests" }],
          ]
        }
      },
      {
        type = "metric", x = 16, y = 9, width = 8, height = 6
        properties = {
          title  = "Lambda duration (ms)"
          region = var.region
          period = 300
          metrics = [
            ["AWS/Lambda", "Duration", "FunctionName", aws_lambda_function.api.function_name, { stat = "Average", label = "API average" }],
            ["AWS/Lambda", "Duration", "FunctionName", aws_lambda_function.api.function_name, { stat = "Maximum", label = "API max" }],
          ]
        }
      },
    ]
  })
}
