#!/bin/sh
# One-time setup: creates the S3 bucket that stores Terraform state, so the state
# is shared by the team and the CI/CD pipeline instead of living on one laptop.
# Run once per AWS account, before `terraform init`.
set -eu
REGION=us-east-1
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
BUCKET="campuspulse-tfstate-$ACCOUNT"

aws s3api create-bucket --bucket "$BUCKET" --region "$REGION" >/dev/null
aws s3api put-bucket-tagging --bucket "$BUCKET" --tagging 'TagSet=[{Key=Project,Value=CampusPulse},{Key=ManagedBy,Value=bootstrap-state.sh}]'

# Keep every previous version of the state, so a bad apply can be rolled back.
aws s3api put-bucket-versioning --bucket "$BUCKET" --versioning-configuration Status=Enabled

# Old versions are deleted after 90 days, so storage never grows without limit.
aws s3api put-bucket-lifecycle-configuration --bucket "$BUCKET" --lifecycle-configuration '{
  "Rules": [{"ID": "expire-old-state-versions", "Status": "Enabled", "Filter": {},
             "NoncurrentVersionExpiration": {"NoncurrentDays": 90}}]}'

# State contains secrets: never public, encrypted at rest, HTTPS only.
aws s3api put-public-access-block --bucket "$BUCKET" --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-encryption --bucket "$BUCKET" --server-side-encryption-configuration \
  '{"Rules": [{"ApplyServerSideEncryptionByDefault": {"SSEAlgorithm": "AES256"}}]}'
aws s3api put-bucket-policy --bucket "$BUCKET" --policy "{
  \"Version\": \"2012-10-17\",
  \"Statement\": [{\"Sid\": \"HttpsOnly\", \"Effect\": \"Deny\", \"Principal\": \"*\", \"Action\": \"s3:*\",
    \"Resource\": [\"arn:aws:s3:::$BUCKET\", \"arn:aws:s3:::$BUCKET/*\"],
    \"Condition\": {\"Bool\": {\"aws:SecureTransport\": \"false\"}}}]}"

echo "Created s3://$BUCKET"
