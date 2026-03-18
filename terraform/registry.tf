resource "yandex_container_registry" "main" {
  name      = var.registry_name
  folder_id = var.folder_id
}
