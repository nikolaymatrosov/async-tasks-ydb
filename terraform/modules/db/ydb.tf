resource "yandex_ydb_database_dedicated" "main" {
  name       = var.ydb_name
  folder_id  = var.folder_id
  network_id = yandex_vpc_network.main.id
  subnet_ids = [for s in yandex_vpc_subnet.main : s.id]

  resource_preset_id = var.ydb_resource_preset
  scale_policy {
    fixed_scale {
      size = var.ydb_fixed_size
    }
  }

  storage_config {
    storage_type_id = var.ydb_storage_type
    group_count     = var.ydb_storage_groups
  }

  location {
    region {
      id = "ru-central1"
    }
  }
}

data "dirhash_sha256" "migrations" {
  directory = "${path.module}/../../../migrations"
}
