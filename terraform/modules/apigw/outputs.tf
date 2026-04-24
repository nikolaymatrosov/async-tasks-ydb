output "gateway_id" {
  description = "API Gateway resource ID"
  value       = yandex_api_gateway.main.id
}

output "gateway_domain" {
  description = "Default domain assigned to the API Gateway"
  value       = yandex_api_gateway.main.domain
}
