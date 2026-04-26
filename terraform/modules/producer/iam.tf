resource "yandex_iam_service_account" "producer_ig" {
  name      = "async-tasks-producer-ig-sa"
  folder_id = var.folder_id
}

resource "yandex_iam_service_account" "producer_vm" {
  name      = "async-tasks-producer-vm-sa"
  folder_id = var.folder_id
}

resource "yandex_resourcemanager_folder_iam_member" "producer_ig_compute_editor" {
  folder_id = var.folder_id
  role      = "compute.editor"
  member    = "serviceAccount:${yandex_iam_service_account.producer_ig.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "producer_ig_sa_user" {
  folder_id = var.folder_id
  role      = "iam.serviceAccounts.user"
  member    = "serviceAccount:${yandex_iam_service_account.producer_ig.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "producer_ig_vpc_user" {
  folder_id = var.folder_id
  role      = "vpc.user"
  member    = "serviceAccount:${yandex_iam_service_account.producer_ig.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "producer_ig_vpc_public_admin" {
  folder_id = var.folder_id
  role      = "vpc.publicAdmin"
  member    = "serviceAccount:${yandex_iam_service_account.producer_ig.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "producer_vm_registry_puller" {
  folder_id = var.folder_id
  role      = "container-registry.images.puller"
  member    = "serviceAccount:${yandex_iam_service_account.producer_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "producer_vm_ydb_editor" {
  folder_id = var.folder_id
  role      = "ydb.editor"
  member    = "serviceAccount:${yandex_iam_service_account.producer_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "producer_vm_monitoring_editor" {
  folder_id = var.folder_id
  role      = "monitoring.editor"
  member    = "serviceAccount:${yandex_iam_service_account.producer_vm.id}"
}
