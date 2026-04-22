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
  coordinator_image = "${var.registry_url}/coordinator:${data.external.git_hash.result.sha}"
}

resource "null_resource" "coordinator_image" {
  triggers = {
    git_sha = data.external.git_hash.result.sha
  }

  provisioner "local-exec" {
    command = "cd ${path.module}/../../.. && docker build --platform linux/amd64 --build-arg EXAMPLE=04_coordinated_table -t ${local.coordinator_image} . && docker push ${local.coordinator_image}"
  }
}
