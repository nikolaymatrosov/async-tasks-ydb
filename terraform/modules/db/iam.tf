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
