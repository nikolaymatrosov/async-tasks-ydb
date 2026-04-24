resource "yandex_api_gateway" "main" {
  name        = var.name
  description = var.description
  folder_id   = var.folder_id
  spec        = var.spec
  labels      = var.labels
}
