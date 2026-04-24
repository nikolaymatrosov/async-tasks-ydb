# Required variables

variable "cloud_id" {
  description = "Yandex Cloud organization cloud ID"
  type        = string
}

variable "folder_id" {
  description = "Yandex Cloud folder ID for all resources"
  type        = string
}


# Optional variables

variable "zone" {
  description = "Yandex Cloud availability zone (used for non-subnet resources)"
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
    # "ru-central1-e" = "10.131.0.0/24"
  }
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

variable "ssh_public_key_path" {
  description = "Local path to the SSH public key for VM access (optional, for debugging)"
  type        = string
  default     = "~/.ssh/id_rsa.pub"
}

variable "ssh_private_key_path" {
  description = "Local path to the SSH private key used by the Terraform SSH provisioner on the bastion"
  type        = string
  default     = "~/.ssh/id_rsa"
}

variable "ydb_name" {
  description = "Name of the YDB Dedicated database"
  type        = string
  default     = "async-tasks-ydb"
}

variable "ydb_resource_preset" {
  description = "YDB compute resource preset (e.g. medium for 8 CPU, 32 GB)"
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

# Feature 005: autoscaling deployment variables

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
  default     = 120
}

variable "ig_measurement_duration" {
  description = "Averaging window in seconds for autoscale"
  type        = number
  default     = 60
}

variable "producer_size" {
  description = "Fixed number of producer VMs"
  type        = number
  default     = 1
}

variable "producer_rate" {
  description = "Task injection rate (tasks/second) for the coordinated tasks producer"
  type        = number
  default     = 500
}

variable "apigw_name" {
  description = "Name of the API Gateway"
  type        = string
  default     = "async-tasks-apigw"
}

variable "apigw_description" {
  description = "Description of the API Gateway"
  type        = string
  default     = ""
}

variable "apigw_spec_file" {
  description = "Path to the OpenAPI 3.0 spec YAML file, relative to the terraform/ directory"
  type        = string
  default     = "apigw-spec.yaml"
}
