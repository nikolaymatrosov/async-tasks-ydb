output "ydb_endpoint" {
  description = "Full gRPC connection string for YDB"
  value       = module.db.ydb_endpoint
}

output "ydb_database_path" {
  description = "YDB database path"
  value       = module.db.ydb_database_path
}

output "registry_id" {
  description = "Container Registry ID (used in image paths: cr.yandex/<id>/...)"
  value       = module.db.registry_id
}

output "instance_group_id" {
  description = "ID of the created workers instance group"
  value       = module.workers.instance_group_id
}


output "cdc_worker_image" {
  description = "Container image for cdc-worker example"
  value       = module.workers.cdc_worker_image
}

output "topic_bench_image" {
  description = "Container image for topic-bench example"
  value       = module.workers.topic_bench_image
}

output "coordinator_image" {
  description = "Container image for coordinator example"
  value       = module.workers.coordinator_image
}

output "migrations_image" {
  description = "Container image for goose migrations"
  value       = module.workers.migrations_image
}

output "migrations_run_cmd" {
  description = "Ready-to-use docker run command for applying migrations"
  value       = module.workers.migrations_run_cmd
}

output "producer_instance_group_id" {
  description = "ID of the producer instance group"
  value       = module.producer.producer_instance_group_id
}

output "bastion_ip" {
  description = "Public IP of the bastion jump host"
  value       = yandex_compute_instance.bastion.network_interface[0].nat_ip_address
}

output "gateway_id" {
  description = "API Gateway resource ID"
  value       = module.apigw.gateway_id
}

output "gateway_domain" {
  description = "Default domain assigned to the API Gateway"
  value       = module.apigw.gateway_domain
}
