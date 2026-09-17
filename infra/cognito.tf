# Step 5: Cognito user pool for signing in to the dashboard and API.
# Roles are Cognito groups; they appear in each user's token as "cognito:groups".

resource "aws_cognito_user_pool" "main" {
  name                = "${var.project}-users"
  user_pool_tier      = "LITE"
  deletion_protection = "INACTIVE"

  # Users sign in with their email address.
  username_attributes = ["email"]

  # Only administrators create accounts; there is no public sign-up.
  admin_create_user_config {
    allow_admin_create_user_only = true
  }

  password_policy {
    minimum_length                   = 8
    require_lowercase                = true
    require_numbers                  = true
    require_uppercase                = false
    require_symbols                  = false
    temporary_password_validity_days = 7
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "admin_only"
      priority = 1
    }
  }
}

# Cognito's hosted sign-in page lives on this domain. Version 1 is the classic
# hosted UI, included in the Lite tier.
resource "random_string" "auth_domain" {
  length  = 6
  special = false
  upper   = false
}

resource "aws_cognito_user_pool_domain" "main" {
  domain                = "${var.project}-${random_string.auth_domain.result}"
  user_pool_id          = aws_cognito_user_pool.main.id
  managed_login_version = 1
}

# App client for the dashboard and the login Lambda. It has no client secret.
# The dashboard signs users in through the hosted page (authorization code flow
# with PKCE); the login Lambda uses username/password sign-in for API tools.
resource "aws_cognito_user_pool_client" "api" {
  name         = "${var.project}-api"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret               = false
  explicit_auth_flows           = ["ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"]
  prevent_user_existence_errors = "ENABLED"

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "email", "profile"]
  supported_identity_providers         = ["COGNITO"]
  # Cognito only sends users back to these dashboard addresses.
  callback_urls = [for origin in local.dashboard_origins : "${origin}/callback"]
  logout_urls   = [for origin in local.dashboard_origins : "${origin}/login"]

  id_token_validity      = 1
  access_token_validity  = 1
  refresh_token_validity = 1
  token_validity_units {
    id_token      = "hours"
    access_token  = "hours"
    refresh_token = "days"
  }
}

resource "aws_cognito_user_group" "role" {
  for_each = toset(["student", "staff", "admin"])

  name         = each.key
  user_pool_id = aws_cognito_user_pool.main.id
  description  = "CampusPulse ${each.key} role"
}

# Demo accounts, the same ones the local setup command creates.
locals {
  demo_users = {
    "student@northbridge.edu"  = { name = "Sam Rivera", role = "student" }
    "student2@northbridge.edu" = { name = "Taylor Kim", role = "student" }
    "staff@northbridge.edu"    = { name = "Jordan Lee", role = "staff" }
    "admin@northbridge.edu"    = { name = "Alex Morgan", role = "admin" }
  }
}

resource "aws_cognito_user" "demo" {
  for_each = local.demo_users

  user_pool_id = aws_cognito_user_pool.main.id
  username     = each.key
  password     = var.demo_password # a permanent password, so no reset is forced at first sign-in

  attributes = {
    email          = each.key
    email_verified = true
    name           = each.value.name
  }
}

resource "aws_cognito_user_in_group" "demo" {
  for_each = local.demo_users

  user_pool_id = aws_cognito_user_pool.main.id
  username     = aws_cognito_user.demo[each.key].username
  group_name   = aws_cognito_user_group.role[each.value.role].name
}
