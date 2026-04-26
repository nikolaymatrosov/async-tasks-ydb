variable "folder_id" {
  description = "Yandex Cloud folder ID"
  type        = string
}

variable "registry_url" {
  description = "Container Registry base URL (cr.yandex/<id>)"
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

variable "ig_max_size" {
  description = "Maximum instance group size"
  type        = number
  default     = 5
}

variable "ig_min_zone_size" {
  description = "Minimum instances per availability zone"
  type        = number
  default     = 1
}

variable "ig_cpu_target" {
  description = "CPU utilisation target (%) for scale-out"
  type        = number
  default     = 70
}

variable "ig_stabilization_duration" {
  description = "Seconds to wait before allowing scale-in"
  type        = number
  default     = 300
}

variable "ig_warmup_duration" {
  description = "Seconds a new instance is excluded from autoscale averaging"
  type        = number
  default     = 30
}

variable "ig_measurement_duration" {
  description = "Averaging window in seconds for autoscale"
  type        = number
  default     = 60
}
