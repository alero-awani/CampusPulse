# CampusPulse

Cloud-native smart campus operations platform for NorthBridge University: a serverless Go API on AWS Lambda, DynamoDB, Cognito and API Gateway, with a React dashboard on S3 + CloudFront, deployed with Terraform.

## Repository layout

| Folder | Contents |
|---|---|
| `backend/` | Go API (Lambda) |
| `campus-pulse-dashboard/` | React dashboard (Bun + Vite) |
| `infra/` | Terraform and deploy scripts |
| `docs/` | Architecture and CI/CD documentation |
| `.github/workflows/` | CI and deployment pipelines |

## CI/CD

Every pull request is checked automatically, and every merge to `main` deploys to AWS with no stored access keys.

| Workflow | Trigger | What it does |
|---|---|---|
| `ci.yml` | Pull request | `go vet` + `go test`, `terraform fmt` + `validate`, dashboard build (no AWS access) |
| `deploy.yml` | Push to `main` | Build Lambda → `terraform plan` → `apply` → deploy dashboard → `/health` check |

Full details, setup and troubleshooting: [docs/ci-cd.md](docs/ci-cd.md)

## Contributors

| Name | Contribution |
|---|---|
| Awani Alero ([@alero-awani](https://github.com/alero-awani)) | Application, infrastructure, Terraform state setup |
| Tanushka Indresh Gupta ([@18tanu](https://github.com/18tanu)) | CI/CD pipeline, AWS OIDC authentication, least-privilege IAM, deployment hardening |
