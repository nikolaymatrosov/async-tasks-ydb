resource "yandex_vpc_network" "main" {
  name      = "async-tasks-net"
  folder_id = var.folder_id
}

resource "yandex_vpc_subnet" "main" {
  for_each       = var.subnet_cidrs
  name           = "async-tasks-subnet-${each.key}"
  zone           = each.key
  network_id     = yandex_vpc_network.main.id
  v4_cidr_blocks = [each.value]
  folder_id      = var.folder_id
}
