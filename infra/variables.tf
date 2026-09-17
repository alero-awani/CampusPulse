variable "region" {
  description = "AWS region for all resources."
  type        = string
  default     = "us-east-1"
}

variable "project" {
  description = "Prefix for resource names."
  type        = string
  default     = "campuspulse"
}

variable "budget_email" {
  description = "Email address that receives budget alerts."
  type        = string
}

variable "monthly_budget_usd" {
  description = "Monthly spending limit watched by the budget alert, in USD."
  type        = number
  default     = 5
}

variable "demo_password" {
  description = "Password for the demo accounts in Cognito."
  type        = string
  sensitive   = true
}

variable "cors_allowed_origins" {
  description = "Browser origins allowed to call the API (the dashboard's address)."
  type        = list(string)
  default     = ["http://localhost:8081", "http://localhost:5173"]
}
