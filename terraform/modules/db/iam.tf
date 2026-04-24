resource "yandex_iam_service_account" "coi_vm" {
  name      = "coi-vm-sa"
  folder_id = var.folder_id
}

resource "yandex_resourcemanager_folder_iam_member" "registry_puller" {
  folder_id = var.folder_id
  role      = "container-registry.images.puller"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "ydb_editor" {
  folder_id = var.folder_id
  role      = "ydb.editor"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "monitoring_editor" {
  folder_id = var.folder_id
  role      = "monitoring.editor"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "vpc_user" {
  folder_id = var.folder_id
  role      = "vpc.user"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "vpc_public_admin" {
  folder_id = var.folder_id
  role      = "vpc.publicAdmin"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "compute_editor" {
  folder_id = var.folder_id
  role      = "compute.editor"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "iam_sa_user" {
  folder_id = var.folder_id
  role      = "iam.serviceAccounts.user"
  member    = "serviceAccount:${yandex_iam_service_account.coi_vm.id}"
}

resource "yandex_iam_service_account" "bastion" {
  name      = "async-tasks-bastion-sa"
  folder_id = var.folder_id
}

resource "yandex_resourcemanager_folder_iam_member" "bastion_ydb_editor" {
  folder_id = var.folder_id
  role      = "ydb.editor"
  member    = "serviceAccount:${yandex_iam_service_account.bastion.id}"
}

resource "yandex_resourcemanager_folder_iam_member" "bastion_registry_puller" {
  folder_id = var.folder_id
  role      = "container-registry.images.puller"
  member    = "serviceAccount:${yandex_iam_service_account.bastion.id}"
}
