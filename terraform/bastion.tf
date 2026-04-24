data "yandex_compute_image" "coi_bastion" {
  family = "container-optimized-image"
}

resource "yandex_compute_instance" "bastion" {
  name               = "async-tasks-bastion"
  folder_id          = var.folder_id
  zone               = var.zone
  platform_id        = "standard-v3"
  service_account_id = module.db.bastion_service_account_id

  resources {
    cores  = 2
    memory = 2
  }

  boot_disk {
    initialize_params {
      image_id = data.yandex_compute_image.coi_bastion.id
      size     = 20
    }
  }

  network_interface {
    subnet_id      = module.db.subnet_ids[0]
    nat            = true
    nat_ip_address = "111.88.240.80"
  }

  metadata = {
    ssh-keys = "yc-user:${file(var.ssh_public_key_path)}"
  }

  depends_on = [module.workers]
}

resource "null_resource" "run_migrations" {
  triggers = {
    migrations_image = module.workers.migrations_image
  }

  connection {
    type        = "ssh"
    user        = "yc-user"
    host        = yandex_compute_instance.bastion.network_interface[0].nat_ip_address
    private_key = file(var.ssh_private_key_path)
  }

  provisioner "remote-exec" {
    inline = [
      "docker login --username iam --password $(yc iam create-token) cr.yandex",
      "docker run --rm -e YDB_ENDPOINT='${module.db.ydb_endpoint}' ${module.workers.migrations_image}",
    ]
  }

  depends_on = [yandex_compute_instance.bastion]
}
