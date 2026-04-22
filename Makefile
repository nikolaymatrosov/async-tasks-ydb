.PHONY: migrate migrate-status migrate-up-one migrate-down migrate-reset migrate-redo \
       docker-login docker-build docker-push deploy docker-pull-command \
       docker-build-migrations docker-push-migrations docker-migrate-cmd \
       docker-coordinator-cmd docker-coordinator-producer-cmd

# Load environment variables from .env file
include .env
export

# Build the YDB connection string with goose parameters and IAM token
YDB_ENDPOINT := $(shell cd terraform && terraform output -raw ydb_endpoint 2>/dev/null)
YDB_GRPC_ENDPOINT := $(shell printf '%s\n' "$(YDB_ENDPOINT)" | sed 's?[?].*$$??')
YDB_DATABASE_PATH := $(shell cd terraform && terraform output -raw ydb_database_path 2>/dev/null)
YDB_CONNECTION_STRING := $(YDB_ENDPOINT)&go_query_mode=scripting&go_fake_tx=scripting&go_query_bind=declare,numeric&token=`yc iam create-token`&ca-file=/etc/ssl/certs/ydb-ca.crt

# Apply all pending migrations
migrate:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" up

# Check migration status
migrate-status:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" status

# Apply one migration
migrate-up-one:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" up-by-one

# Rollback the last migration
migrate-down:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" down

# Rollback all migrations
migrate-reset:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" reset

# Re-apply the latest migration
migrate-redo:
	@cd migrations && goose ydb "$(YDB_CONNECTION_STRING)" redo

# --- Docker targets ---

REGISTRY_ID := $(shell cd terraform && terraform output -raw registry_id 2>/dev/null)
EXAMPLES := 01_db_producer 02_cdc_worker 03_topic

# Authenticate Docker to Yandex Container Registry
docker-login:
	cat sa.json | docker login --username json_key --password-stdin cr.yandex

# Build all example container images
docker-build:
	@for example in $(EXAMPLES); do \
		echo "Building $$example..."; \
		docker build --build-arg EXAMPLE=$$example -t cr.yandex/$(REGISTRY_ID)/$$example:latest .; \
	done

# Push all example container images to registry
docker-push:
	@for example in $(EXAMPLES); do \
		echo "Pushing $$example..."; \
		docker push cr.yandex/$(REGISTRY_ID)/$$example:latest; \
	done

# Full deployment: terraform apply -> build -> push -> migrate
deploy:
	cd terraform && terraform apply
	$(MAKE) docker-build
	$(MAKE) docker-push
	$(MAKE) migrate

docker-pull-command:
	@for img in db_producer_image cdc_worker_image topic_bench_image migrations_image coordinator_image; do \
		echo "docker pull $$(cd terraform && terraform output -raw $$img)"; \
	done

MIGRATIONS_IMAGE := $(shell cd terraform && terraform output -raw migrations_image 2>/dev/null)	
COORDINATOR_IMAGE := $(shell cd terraform && terraform output -raw coordinator_image 2>/dev/null)

# Build migrations container image
docker-build-migrations:
	docker build -f Dockerfile.migrations -t $(MIGRATIONS_IMAGE) .	
# Push migrations container image to registry
docker-push-migrations:
	docker push $(MIGRATIONS_IMAGE)

# Print ready-to-use docker run command for migrations
docker-migrate-cmd:
	@echo "docker run --rm $(MIGRATIONS_IMAGE) '$(YDB_CONNECTION_STRING)' up"

# Print ready-to-use docker run command for the 04_coordinated_table example on the VM
docker-coordinator-cmd:
	@echo "docker run --rm -e YDB_ENDPOINT='$(YDB_GRPC_ENDPOINT)' $(COORDINATOR_IMAGE) --database '$(YDB_DATABASE_PATH)' --mode worker"

# Print ready-to-use docker run command for the 04_coordinated_table producer on the VM
docker-coordinator-producer-cmd:
	@echo "docker run --rm -e YDB_ENDPOINT='$(YDB_GRPC_ENDPOINT)' $(COORDINATOR_IMAGE) --database '$(YDB_DATABASE_PATH)' --mode producer"
