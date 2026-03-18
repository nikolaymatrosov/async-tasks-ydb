data "yandex_compute_image" "coi" {
  family = "container-optimized-image"
}

resource "yandex_compute_instance" "coi_vm" {
  name               = "async-tasks-vm"
  zone               = var.zone
  service_account_id = yandex_iam_service_account.coi_vm.id

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
    subnet_id = yandex_vpc_subnet.main.id
    nat       = true
  }

  metadata = {
    user-data = templatefile("${path.module}/cloud-init.yaml", {
      ssh_public_key = var.ssh_public_key
    })
  }
}
