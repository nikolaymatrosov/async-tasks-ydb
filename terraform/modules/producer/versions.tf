terraform {
  required_providers {
    yandex = {
      source = "yandex-cloud/yandex"
    }
    external = {
      source  = "hashicorp/external"
      version = ">= 2.0"
    }
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0"
    }
  }
}
