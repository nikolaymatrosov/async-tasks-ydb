resource "yandex_ydb_database_serverless" "main" {
  name        = var.ydb_name
  folder_id   = var.folder_id
  location_id = "ru-central1"
}
