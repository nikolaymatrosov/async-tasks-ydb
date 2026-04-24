terraform {
  required_version = ">= 1.5"

  required_providers {
    yandex = {
      source = "yandex-cloud/yandex"
    }
    dirhash = {
      source = "Think-iT-Labs/dirhash"
    }
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0"
    }
    external = {
      source  = "hashicorp/external"
      version = ">= 2.0"
    }
  }
}

provider "yandex" {
  cloud_id  = var.cloud_id
  folder_id = var.folder_id
  zone      = var.zone
}

module "db" {
  source = "./modules/db"

  cloud_id            = var.cloud_id
  folder_id           = var.folder_id
  zone                = var.zone
  subnet_cidrs        = var.subnet_cidrs
  ydb_name            = var.ydb_name
  ydb_resource_preset = var.ydb_resource_preset
  ydb_fixed_size      = var.ydb_fixed_size
  ydb_storage_type    = var.ydb_storage_type
  ydb_storage_groups  = var.ydb_storage_groups
  registry_name       = var.registry_name
}

module "workers" {
  source = "./modules/workers"

  folder_id                 = var.folder_id
  zone                      = var.zone
  platform_id               = var.platform_id
  vm_cores                  = var.vm_cores
  vm_memory                 = var.vm_memory
  ssh_public_key            = var.ssh_public_key
  ig_max_size               = var.ig_max_size
  ig_min_zone_size          = var.ig_min_zone_size
  ig_cpu_target             = var.ig_cpu_target
  ig_stabilization_duration = var.ig_stabilization_duration
  ig_warmup_duration        = var.ig_warmup_duration
  ig_measurement_duration   = var.ig_measurement_duration
  registry_url              = module.db.registry_url
  service_account_id        = module.db.service_account_id
  ydb_endpoint              = module.db.ydb_endpoint
  ydb_database              = module.db.ydb_database_path
  subnet_ids                = module.db.subnet_ids
}

module "producer" {
  source = "./modules/producer"

  folder_id      = var.folder_id
  zone           = var.zone
  platform_id    = var.platform_id
  vm_cores       = var.vm_cores
  vm_memory      = var.vm_memory
  ssh_public_key = var.ssh_public_key
  producer_size  = var.producer_size
  producer_rate  = var.producer_rate

  registry_url       = module.db.registry_url
  service_account_id = module.db.service_account_id
  ydb_endpoint       = module.db.ydb_endpoint
  ydb_database       = module.db.ydb_database_path
  subnet_ids         = module.db.subnet_ids
  apigw_url          = module.apigw.gateway_domain
}

module "apigw" {
  source = "./modules/apigw"

  name        = var.apigw_name
  description = var.apigw_description
  folder_id   = var.folder_id
  spec        = file("${path.module}/${var.apigw_spec_file}")
}
