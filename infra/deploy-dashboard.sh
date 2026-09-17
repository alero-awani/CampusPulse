#!/bin/sh
# Builds the dashboard, uploads it to the S3 bucket, and clears CloudFront's cache
# so visitors get the new version. Run after `terraform apply`.
set -eu
cd "$(dirname "$0")"
BUCKET=$(terraform output -raw dashboard_bucket)
DISTRIBUTION=$(terraform output -raw dashboard_distribution_id)
URL=$(terraform output -raw dashboard_url)

cd ../campus-pulse-dashboard
bun run build
cp dist/client/_shell.html dist/client/index.html
aws s3 sync dist/client "s3://$BUCKET" --delete
aws cloudfront create-invalidation --distribution-id "$DISTRIBUTION" --paths "/*" >/dev/null
echo "Dashboard deployed: $URL"
