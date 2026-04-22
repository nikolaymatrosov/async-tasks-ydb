output "ydb_endpoint" {
  description = "Full gRPC connection string for YDB"
  value       = yandex_ydb_database_dedicated.main.ydb_full_endpoint
}

output "ydb_database_path" {
  description = "YDB database path"
  value       = yandex_ydb_database_dedicated.main.database_path
}

output "registry_id" {
  description = "Container Registry ID (used in image paths: cr.yandex/<id>/...)"
  value       = yandex_container_registry.main.id
}

output "vm_external_ip" {
  description = "Public IP of the COI VM (for SSH access)"
  value       = yandex_compute_instance.coi_vm.network_interface[0].nat_ip_address
}

output "vm_internal_ip" {
  description = "Private IP of the COI VM"
  value       = yandex_compute_instance.coi_vm.network_interface[0].ip_address
}

output "service_account_id" {
  description = "ID of the created service account"
  value       = yandex_iam_service_account.coi_vm.id
}

output "db_producer_image" {
  description = "Container image for db-producer example"
  value       = local.db_producer_image
}

output "cdc_worker_image" {
  description = "Container image for cdc-worker example"
  value       = local.cdc_worker_image
}

output "topic_bench_image" {
  description = "Container image for topic-bench example"
  value       = local.topic_bench_image
}

output "coordinator_image" {
  description = "Container image for coordinator example"
  value       = local.coordinator_image
}

output "migrations_image" {
  description = "Container image for goose migrations"
  value       = local.migrations_image
}

output "migrations_run_cmd" {
  description = "Ready-to-use docker run command for applying migrations"
  value       = "docker run --rm ${local.migrations_image} '${yandex_ydb_database_dedicated.main.ydb_full_endpoint}?token='$(yc iam create-token)'&go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric' up"
}
