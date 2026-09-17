# Step 4: private network for the Lambda functions.
#
# There is no internet gateway and no NAT gateway: code running in these subnets
# cannot reach the internet. It reaches DynamoDB through a gateway endpoint,
# which keeps traffic on the AWS network and is free.

data "aws_caller_identity" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project}-vpc" }
}

# Two subnets in different availability zones, so Lambda keeps running if one zone fails.
resource "aws_subnet" "private" {
  count = 2

  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(aws_vpc.main.cidr_block, 8, count.index + 1) # 10.0.1.0/24, 10.0.2.0/24
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = false

  tags = { Name = "${var.project}-private-${data.aws_availability_zones.available.names[count.index]}" }
}

# Only local VPC traffic and the DynamoDB endpoint route; no route to the internet.
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  tags = { Name = "${var.project}-private" }
}

resource "aws_route_table_association" "private" {
  count = 2

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.region}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  # Traffic through this endpoint can only reach the CampusPulse tables.
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = "*"
      Action    = "dynamodb:*"
      Resource = [
        "arn:aws:dynamodb:${var.region}:${data.aws_caller_identity.current.account_id}:table/${var.project}-*",
        "arn:aws:dynamodb:${var.region}:${data.aws_caller_identity.current.account_id}:table/${var.project}-*/index/*",
      ]
    }]
  })

  tags = { Name = "${var.project}-dynamodb" }
}

# Security group for the Lambda functions: nothing can connect in, and they can
# only connect out to DynamoDB over HTTPS.
resource "aws_security_group" "lambda" {
  name        = "${var.project}-lambda"
  description = "CampusPulse Lambda functions: no inbound, HTTPS to DynamoDB only"
  vpc_id      = aws_vpc.main.id

  tags = { Name = "${var.project}-lambda" }
}

resource "aws_vpc_security_group_egress_rule" "lambda_to_dynamodb" {
  security_group_id = aws_security_group.lambda.id
  description       = "HTTPS to DynamoDB through the gateway endpoint"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  prefix_list_id    = aws_vpc_endpoint.dynamodb.prefix_list_id
}

# Take away the default security group's allow-all rules, so nothing placed in
# this VPC by mistake gets open access.
resource "aws_default_security_group" "default" {
  vpc_id = aws_vpc.main.id

  tags = { Name = "${var.project}-default-locked" }
}
