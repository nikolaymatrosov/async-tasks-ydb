resource "yandex_container_registry" "coi-traefik" {
  folder_id = var.folder_id
  name = "coi-traefik"
}

data "external" "git_hash" {
  program = [
    "git",
    "log",
    "--pretty={\"sha\":\"%H\"}",
    "-1",
    "HEAD"
  ]
}

locals {
  sidecar_image = "cr.yandex/${yandex_container_registry.coi-traefik.id}/yc-traefik-http-provider:${data.external.git_hash.result.sha}"
  traefik_image = "cr.yandex/${yandex_container_registry.coi-traefik.id}/traefik:v3.3"
  httpbin_image = "cr.yandex/${yandex_container_registry.coi-traefik.id}/go-httpbin:v2.9.0"
}

resource "null_resource" "build" {
  triggers = {
    git_sha = data.external.git_hash.result.sha
  }

  provisioner "local-exec" {
    command = "cd .. && docker build --platform linux/amd64 -t ${local.sidecar_image} . && docker push ${local.sidecar_image}"
  }
}

resource "null_resource" "traefik" {
    provisioner "local-exec" {
        command = "docker pull --platform linux/amd64 traefik:v3.3 && docker tag traefik:v3.3 ${local.traefik_image} && docker push ${local.traefik_image}"
    }
}

resource "null_resource" "httpbin" {
    provisioner "local-exec" {
        command = "docker pull --platform linux/amd64 mccutchen/go-httpbin:v2.9.0 && docker tag mccutchen/go-httpbin:v2.9.0 ${local.httpbin_image} && docker push ${local.httpbin_image}"
    }
}