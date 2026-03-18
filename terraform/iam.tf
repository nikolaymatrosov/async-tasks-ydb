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
