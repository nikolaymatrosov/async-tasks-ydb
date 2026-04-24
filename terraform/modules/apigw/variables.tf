variable "name" {
  description = "Name of the API Gateway"
  type        = string
}

variable "description" {
  description = "Human-readable description of the API Gateway"
  type        = string
  default     = ""
}

variable "folder_id" {
  description = "Yandex Cloud folder ID for the API Gateway resource"
  type        = string
}

variable "spec" {
  description = "OpenAPI 3.0 YAML specification content (pre-read string)"
  type        = string
}

variable "labels" {
  description = "Key-value labels to attach to the API Gateway"
  type        = map(string)
  default     = {}
}
