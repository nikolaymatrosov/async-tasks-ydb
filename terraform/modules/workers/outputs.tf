output "instance_group_id" {
  description = "ID of the workers instance group"
  value       = yandex_compute_instance_group.workers.id
}

output "coordinator_image" {
  description = "Container image for coordinator example"
  value       = local.coordinator_image
}

output "cdc_worker_image" {
  description = "Container image for cdc-worker example"
  value       = local.cdc_worker_image
}

output "topic_bench_image" {
  description = "Container image for topic-bench example"
  value       = local.topic_bench_image
}

output "migrations_image" {
  description = "Container image for goose migrations"
  value       = local.migrations_image
}

output "migrations_run_cmd" {
  description = "Ready-to-use docker run command for applying migrations"
  value       = "docker run --rm ${local.migrations_image} '${var.ydb_endpoint}?token='$(yc iam create-token)'&go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric' up"
}
