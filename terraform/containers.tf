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
  registry_url      = "cr.yandex/${yandex_container_registry.main.id}"
  db_producer_image = "${local.registry_url}/db-producer:${data.external.git_hash.result.sha}"
  cdc_worker_image  = "${local.registry_url}/cdc-worker:${data.external.git_hash.result.sha}"
  topic_bench_image = "${local.registry_url}/topic-bench:${data.external.git_hash.result.sha}"
  migrations_image  = "${local.registry_url}/migrations:${data.external.git_hash.result.sha}"
}

resource "null_resource" "db_producer_image" {
  triggers = {
    git_sha = data.external.git_hash.result.sha
  }

  provisioner "local-exec" {
    command = "cd ${path.module}/.. && docker build --platform linux/amd64 --build-arg EXAMPLE=01_db_producer -t ${local.db_producer_image} . && docker push ${local.db_producer_image}"
  }
}

resource "null_resource" "cdc_worker_image" {
  triggers = {
    git_sha = data.external.git_hash.result.sha
  }

  provisioner "local-exec" {
    command = "cd ${path.module}/.. && docker build --platform linux/amd64 --build-arg EXAMPLE=02_cdc_worker -t ${local.cdc_worker_image} . && docker push ${local.cdc_worker_image}"
  }
}

resource "null_resource" "topic_bench_image" {
  triggers = {
    git_sha = data.external.git_hash.result.sha
  }

  provisioner "local-exec" {
    command = "cd ${path.module}/.. && docker build --platform linux/amd64 --build-arg EXAMPLE=03_topic -t ${local.topic_bench_image} . && docker push ${local.topic_bench_image}"
  }
}

resource "null_resource" "migrations_image" {
  triggers = {
    git_sha = data.external.git_hash.result.sha
  }

  provisioner "local-exec" {
    command = "cd ${path.module}/.. && docker build --platform linux/amd64 -f Dockerfile.migrations -t ${local.migrations_image} . && docker push ${local.migrations_image}"
  }
}
