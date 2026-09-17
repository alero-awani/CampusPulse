# Step 2: DynamoDB tables. Keys and indexes match backend/internal/store/store.go.
locals {
  tables = {
    events = {
      hash_key  = "event_id"
      range_key = null
      ttl       = "expires_at" # raw events expire after 30 days
      indexes = {
        by_time = { hash_key = "day", range_key = "ts" }
        by_room = { hash_key = "room_id", range_key = "ts" }
      }
    }
    rooms = {
      hash_key  = "room_id"
      range_key = null
      ttl       = null
      indexes   = {}
    }
    alerts = {
      hash_key  = "alert_id"
      range_key = null
      ttl       = null
      indexes = {
        by_status = { hash_key = "status", range_key = "created_key" }
        by_time   = { hash_key = "kind", range_key = "created_key" }
      }
    }
    service-requests = {
      hash_key  = "request_id"
      range_key = null
      ttl       = null
      indexes = {
        by_status  = { hash_key = "status", range_key = "created_key" }
        by_time    = { hash_key = "kind", range_key = "created_key" }
        by_creator = { hash_key = "creator_id", range_key = "created_key" }
      }
    }
    aggregates = {
      hash_key  = "pk"
      range_key = "sk"
      ttl       = "expires_at" # ingestion counters expire after 48 hours
      indexes   = {}
    }
    users = {
      hash_key  = "email"
      range_key = null
      ttl       = null
      indexes   = {}
    }
    settings = {
      hash_key  = "id"
      range_key = null
      ttl       = null
      indexes   = {}
    }
  }
}

resource "aws_dynamodb_table" "this" {
  for_each = local.tables

  name         = "${var.project}-${each.key}"
  billing_mode = "PAY_PER_REQUEST" # pay only for reads and writes made
  hash_key     = each.value.hash_key
  range_key    = each.value.range_key

  # Every key attribute used by the table or its indexes is a string.
  dynamic "attribute" {
    for_each = toset(compact(concat(
      [each.value.hash_key, each.value.range_key],
      flatten([for ix in values(each.value.indexes) : [ix.hash_key, ix.range_key]]),
    )))
    content {
      name = attribute.value
      type = "S"
    }
  }

  dynamic "global_secondary_index" {
    for_each = each.value.indexes
    content {
      name            = global_secondary_index.key
      projection_type = "ALL"

      key_schema {
        attribute_name = global_secondary_index.value.hash_key
        key_type       = "HASH"
      }
      key_schema {
        attribute_name = global_secondary_index.value.range_key
        key_type       = "RANGE"
      }
    }
  }

  dynamic "ttl" {
    for_each = each.value.ttl == null ? [] : [each.value.ttl]
    content {
      attribute_name = ttl.value
      enabled        = true
    }
  }

  # Continuous backups: any table can be restored to any second in the last 35 days.
  point_in_time_recovery {
    enabled = true
  }

  # Data is encrypted at rest with an AWS-owned key (DynamoDB default, free).
}
