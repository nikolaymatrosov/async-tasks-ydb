data "yandex_compute_image" "coi" {
  family = "container-optimized-image"
}

resource "yandex_compute_instance_group" "workers" {
  name               = "async-tasks-workers"
  folder_id          = var.folder_id
  service_account_id = yandex_iam_service_account.coi_vm.id

  instance_template {
    platform_id = var.platform_id

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
      subnet_ids = [for s in yandex_vpc_subnet.main : s.id]
      nat        = true
    }

    metadata = {
      "docker-compose" = templatefile("${path.module}/docker-compose.yml.tpl", {
        coordinator_image = local.coordinator_image
        ydb_endpoint      = yandex_ydb_database_dedicated.main.ydb_full_endpoint
        ydb_database      = yandex_ydb_database_dedicated.main.database_path
        worker_rate       = var.worker_rate
      })
      "user-data" = <<-EOT
        #cloud-config
        write_files:
          - path: /etc/yandex-unified-agent/config.yml
            permissions: '0644'
            content: |
              ${indent(6, templatefile("${path.module}/ua-config.yml.tpl", {
                folder_id   = var.folder_id
                metrics_url = "http://localhost:9090/metrics"
              }))}
        EOT
    }
  }

  scale_policy {
    auto_scale {
      initial_size           = 1
      min_zone_size          = var.ig_min_zone_size
      max_size               = var.ig_max_size
      measurement_duration   = var.ig_measurement_duration
      stabilization_duration = var.ig_stabilization_duration
      warmup_duration        = var.ig_warmup_duration
      cpu_utilization_target = var.ig_cpu_target
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
}
