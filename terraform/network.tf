resource "yandex_vpc_network" "main" {
  name      = "async-tasks-net"
  folder_id = var.folder_id
}

resource "yandex_vpc_subnet" "main" {
  name           = "async-tasks-subnet"
  zone           = var.zone
  network_id     = yandex_vpc_network.main.id
  v4_cidr_blocks = ["10.128.0.0/24"]
  folder_id      = var.folder_id
}
