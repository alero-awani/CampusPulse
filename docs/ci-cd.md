# CI/CD Pipeline

**Author:** Tanushka Indresh Gupta ([@18tanu](https://github.com/18tanu))

This document describes how CampusPulse is tested and deployed, and why it was built this way.

## Overview

```
Pull request ──► ci.yml ──► Go checks · Terraform checks · Dashboard build
                                  (no AWS access)

Merge to main ──► deploy.yml ──► OIDC login ──► Build Lambda ──► terraform plan
                                  ──► terraform apply ──► Deploy dashboard ──► /health check
```

## What was built

### 1. Keyless AWS authentication (OIDC)
- IAM OpenID Connect provider for `token.actions.githubusercontent.com` (audience `sts.amazonaws.com`).
- IAM role `github-actions-campuspulse`, trusted **only** for the `main` branch of this repository.
- GitHub gets short-lived credentials per run. No AWS access keys are stored anywhere.

### 2. Pull request checks (`.github/workflows/ci.yml`)
Runs on every pull request, in three parallel jobs, with no AWS permissions:

| Job | Checks |
|---|---|
| Backend (Go) | `go vet ./...`, `go test ./...` |
| Infra (Terraform) | `terraform fmt -check`, `terraform init -backend=false`, `terraform validate` |
| Dashboard (Bun) | `bun install`, `bun run build` |

### 3. Deployment (`.github/workflows/deploy.yml`)
Runs on every push to `main`:

1. Assume the AWS role via OIDC
2. Build the Lambda binary (`infra/build-lambda.sh`)
3. `terraform plan -out=tfplan`, then `terraform apply tfplan`, so AWS receives exactly what the plan showed
4. Install dashboard dependencies and deploy it (`infra/deploy-dashboard.sh`)
5. Call the API's `/health` endpoint with retries

A `concurrency` group prevents two deployments from running at once.

### 4. Least-privilege permissions
The deploy role uses a custom policy, `campuspulse-deploy-policy`, instead of `AdministratorAccess`:
- S3 access limited to the state bucket and the dashboard bucket
- Lambda, IAM, DynamoDB and SNS limited to `campuspulse-*` resources
- API Gateway limited to `us-east-1`
- Cognito, CloudFront, CloudWatch, Budgets and VPC remain account-wide, because these services can't easily be scoped by name

## Secrets

| Secret | Purpose |
|---|---|
| `AWS_ROLE_ARN` | Role assumed by the deploy workflow |
| `BUDGET_EMAIL` | Receives budget and monitoring alerts (`TF_VAR_budget_email`) |
| `DEMO_PASSWORD` | Cognito demo account password (`TF_VAR_demo_password`) |

## Rules

- Work on a branch and open a pull request. Never push directly to `main`.
- Never commit `terraform.tfstate` or `terraform.tfvars` (both are in `.gitignore`).
- Terraform state lives in `s3://campuspulse-tfstate-278746617511` (versioned, encrypted, with a lock file).

## Troubleshooting log

| Problem | Cause | Fix |
|---|---|---|
| `Not authorized to perform sts:AssumeRoleWithWebIdentity` | This repository uses GitHub's **immutable OIDC subject**, which
