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
  cloud_id                 = var.cloud_id
  folder_id                = var.folder_id
  zone                     = var.zone
  service_account_key_file = var.sa_key_file
}
