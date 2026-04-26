output "ydb_endpoint" {
  description = "Full gRPC connection string for YDB"
  value       = yandex_ydb_database_dedicated.main.ydb_full_endpoint
}

output "ydb_database_path" {
  description = "YDB database path"
  value       = yandex_ydb_database_dedicated.main.database_path
}

output "registry_id" {
  description = "Container Registry ID"
  value       = yandex_container_registry.main.id
}

output "registry_url" {
  description = "Container Registry base URL (cr.yandex/<id>)"
  value       = "cr.yandex/${yandex_container_registry.main.id}"
}

output "subnet_ids" {
  description = "List of subnet IDs created for the network"
  value       = [for s in yandex_vpc_subnet.main : s.id]
}

output "network_id" {
  description = "VPC network ID"
  value       = yandex_vpc_network.main.id
}

output "bastion_service_account_id" {
  description = "ID of the bastion VM service account"
  value       = yandex_iam_service_account.bastion.id
}
