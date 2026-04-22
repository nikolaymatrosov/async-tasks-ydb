variable "cloud_id" {
  description = "Yandex Cloud organization cloud ID"
  type        = string
}

variable "folder_id" {
  description = "Yandex Cloud folder ID for all resources"
  type        = string
}

variable "zone" {
  description = "Yandex Cloud availability zone"
  type        = string
  default     = "ru-central1-a"
}

variable "subnet_cidrs" {
  description = "Map of availability zone to CIDR block for subnets"
  type        = map(string)
  default = {
    "ru-central1-a" = "10.128.0.0/24"
    "ru-central1-b" = "10.129.0.0/24"
    "ru-central1-d" = "10.130.0.0/24"
  }
}

variable "ydb_name" {
  description = "Name of the YDB Dedicated database"
  type        = string
  default     = "async-tasks-ydb"
}

variable "ydb_resource_preset" {
  description = "YDB compute resource preset"
  type        = string
  default     = "medium"
}

variable "ydb_fixed_size" {
  description = "Number of YDB compute nodes"
  type        = number
  default     = 1
}

variable "ydb_storage_type" {
  description = "YDB storage type (ssd or hdd)"
  type        = string
  default     = "ssd"
}

variable "ydb_storage_groups" {
  description = "Number of YDB storage groups"
  type        = number
  default     = 1
}

variable "registry_name" {
  description = "Name of the container registry"
  type        = string
  default     = "async-tasks-registry"
}
