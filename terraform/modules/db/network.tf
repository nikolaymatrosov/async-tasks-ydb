resource "yandex_vpc_network" "main" {
  name      = "async-tasks-net"
  folder_id = var.folder_id
}

resource "yandex_vpc_gateway" "nat" {
  name      = "async-tasks-nat"
  folder_id = var.folder_id

  shared_egress_gateway {}
}

resource "yandex_vpc_route_table" "nat" {
  name       = "async-tasks-route-table"
  folder_id  = var.folder_id
  network_id = yandex_vpc_network.main.id

  static_route {
    destination_prefix = "0.0.0.0/0"
    gateway_id         = yandex_vpc_gateway.nat.id
  }
}

resource "yandex_vpc_subnet" "main" {
  for_each       = var.subnet_cidrs
  name           = "async-tasks-subnet-${each.key}"
  zone           = each.key
  network_id     = yandex_vpc_network.main.id
  v4_cidr_blocks = [each.value]
  folder_id      = var.folder_id
  route_table_id = yandex_vpc_route_table.nat.id
}
