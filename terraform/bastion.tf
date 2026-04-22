data "yandex_compute_image" "ubuntu" {
  family = "ubuntu-2404-lts"
}

resource "yandex_compute_instance" "bastion" {
  name        = "async-tasks-bastion"
  folder_id   = var.folder_id
  zone        = var.zone
  platform_id = "standard-v3"

  resources {
    cores  = 2
    memory = 2
  }

  boot_disk {
    initialize_params {
      image_id = data.yandex_compute_image.ubuntu.id
      size     = 10
    }
  }

  network_interface {
    subnet_id      = module.db.subnet_ids[0]
    nat            = true
    nat_ip_address = "111.88.240.80"
  }

  metadata = {
    ssh-keys = "ubuntu:${var.ssh_public_key}"
  }
}
