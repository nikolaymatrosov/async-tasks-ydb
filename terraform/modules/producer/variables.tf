variable "folder_id" {
  description = "Yandex Cloud folder ID"
  type        = string
}

variable "registry_url" {
  description = "Container Registry base URL (cr.yandex/<id>)"
  type        = string
}

variable "service_account_id" {
  description = "ID of the COI VM service account"
  type        = string
}

variable "ydb_endpoint" {
  description = "Full gRPC connection string for YDB"
  type        = string
}

variable "ydb_database" {
  description = "YDB database path"
  type        = string
}

variable "subnet_ids" {
  description = "List of subnet IDs for VM network interfaces"
  type        = list(string)
}

variable "zone" {
  description = "Yandex Cloud availability zone"
  type        = string
  default     = "ru-central1-a"
}

variable "platform_id" {
  description = "Yandex Cloud platform ID for the COI VM"
  type        = string
  default     = "standard-v4a"
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

variable "producer_size" {
  description = "Fixed number of producer VMs"
  type        = number
  default     = 1
}

variable "producer_rate" {
  description = "Task injection rate (tasks/second) for the coordinated tasks producer"
  type        = number
  default     = 100
}

variable "apigw_url" {
  description = "API Gateway base URL passed to the producer as APIGW_URL"
  type        = string
}
