output "ydb_endpoint" {
  description = "Full gRPC connection string for YDB"
  value       = yandex_ydb_database_serverless.main.ydb_full_endpoint
}

output "ydb_database_path" {
  description = "YDB database path"
  value       = yandex_ydb_database_serverless.main.database_path
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
