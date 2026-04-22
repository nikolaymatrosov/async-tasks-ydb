output "producer_instance_group_id" {
  description = "ID of the producer instance group"
  value       = yandex_compute_instance_group.producer.id
}

output "coordinator_image" {
  description = "Container image for the coordinated tasks producer/worker"
  value       = local.coordinator_image
}
