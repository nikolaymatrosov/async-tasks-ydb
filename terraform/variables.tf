# Required variables

variable "cloud_id" {
  description = "Yandex Cloud organization cloud ID"
  type        = string
}

variable "folder_id" {
  description = "Yandex Cloud folder ID for all resources"
  type        = string
}

variable "sa_key_file" {
  description = "Path to service account key JSON file for Terraform provider auth"
  type        = string
}

# Optional variables

variable "zone" {
  description = "Yandex Cloud availability zone"
  type        = string
  default     = "ru-central1-a"
}

variable "vm_cores" {
  description = "CPU cores for the COI VM"
  type        = number
  default     = 2
}

variable "vm_memory" {
  description = "RAM (GB) for the COI VM"
  type        = number
  default     = 4
}

variable "ssh_public_key" {
  description = "SSH public key for VM access (optional, for debugging)"
  type        = string
  default     = ""
}

variable "ydb_name" {
  description = "Name of the YDB Serverless database"
  type        = string
  default     = "async-tasks-ydb"
}

variable "registry_name" {
  description = "Name of the container registry"
  type        = string
  default     = "async-tasks-registry"
}
