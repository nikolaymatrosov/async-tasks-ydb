data "yandex_compute_image" "coi" {
  family = "container-optimized-image"
}

resource "yandex_compute_instance_group" "producer" {
  name               = "async-tasks-producer"
  folder_id          = var.folder_id
  service_account_id = var.service_account_id

  instance_template {
    platform_id        = var.platform_id
    service_account_id = var.service_account_id

    resources {
      cores  = var.vm_cores
      memory = var.vm_memory
    }

    boot_disk {
      initialize_params {
        image_id = data.yandex_compute_image.coi.id
      }
    }

    network_interface {
      subnet_ids = var.subnet_ids
    }

    metadata = merge(
      {
        "docker-compose" = templatefile("${path.module}/docker-compose.yml.tpl", {
          coordinator_image = local.coordinator_image
          ydb_endpoint      = var.ydb_endpoint
          ydb_database      = var.ydb_database
          producer_rate     = var.producer_rate
          apigw_url         = var.apigw_url
          folder_id         = var.folder_id
        })
        "user-data" = <<-EOT
          #cloud-config
          write_files:
            - path: /home/yc-user/ua-config.yml
              permissions: '0644'
              content: |
                ${indent(6, templatefile("${path.module}/ua-config.yml.tpl", {
  metrics_url = "http://localhost:9090/metrics"
  folder_id   = var.folder_id
}))}
          EOT
      },
      var.ssh_public_key != "" ? { "ssh-keys" = "yc-user:${var.ssh_public_key}" } : {}
    )
  }

  scale_policy {
    fixed_scale {
      size = var.producer_size
    }
  }

  allocation_policy {
    zones = [var.zone]
  }

  deploy_policy {
    max_unavailable = 1
    max_creating    = 1
    max_expansion   = 1
    max_deleting    = 1
  }

  depends_on = [null_resource.coordinator_image]
}
