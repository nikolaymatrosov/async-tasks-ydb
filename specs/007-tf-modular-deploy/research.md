# Research: Terraform Modular Deployment

## Q1: How does Terraform module targeting work for independent deployment?

**Decision**: Use `terraform apply -target=module.<name>` with child modules declared in the root module.

**Rationale**: Terraform's `-target` flag limits the apply to a specific module and its transitive dependencies. With the structure `module "db"`, `module "workers"`, `module "producer"` in the root, an operator can run `terraform apply -target=module.db` to deploy only database resources. Workers and producer depend on `module.db` outputs passed as input variables — Terraform will error cleanly if the database is not yet deployed and those outputs are unknown (the `output` block references won't resolve, so the plan will fail with an actionable message).

**Alternatives considered**:
- Separate Terraform workspaces per component: Rejected — workspaces require separate state files and remote backends, which complicates output sharing and is overkill for a single-team project.
- Separate Terraform root modules in subdirectories: Rejected — would require duplicating provider config and maintaining separate `terraform init` runs. The `-target` approach is simpler and keeps a single state file.

---

## Q2: How should module outputs be passed between child modules?

**Decision**: Root module wires outputs from `module.db` as input variables to `module.workers` and `module.producer`. No data source lookups across module boundaries.

**Rationale**: Explicit input passing makes the dependency graph visible to Terraform's planner. When `module.db` is not yet applied, the outputs are unknown and Terraform refuses to plan `module.workers` without `-target=module.db` having run first. This matches FR-003 and the spec assumption about explicit output consumption.

**Alternatives considered**:
- `terraform_remote_state` data source: Rejected — adds coupling to a remote backend and requires a separate state file per module, contradicting the single-workspace design.
- SSM/Secret Manager lookups: Rejected — unnecessary operational complexity for this repo.

---

## Q3: Should the `data "external" "git_hash"` source be duplicated across modules or live in the root?

**Decision**: Each module that builds Docker images (`workers`, `producer`) owns its own `data "external" "git_hash"` block, and the git SHA is NOT passed from root as a variable.

**Rationale**: Data sources with no dependencies are free to duplicate. If passed from root, the root module would need to declare the external data source and add a variable for it — adding complexity with no benefit. Both child modules will read the same git SHA since they run in the same working directory context (`path.root`).

**Alternatives considered**:
- Root declares git_hash, passes to children: Rejected — adds boilerplate and a variable with no semantic meaning at the module interface level.

---

## Q4: What does the producer compute instance group look like?

**Decision**: The producer uses a **fixed-scale** instance group with `fixed_scale { size = var.producer_size }`, defaulting to 1. It runs the `01_db_producer` container via docker-compose.yml.tpl, injecting `YDB_ENDPOINT` and `YDB_DATABASE` env vars plus `--payload-size` and `--parallelism` flags.

**Rationale**: The producer's role is to inject tasks at a steady rate — horizontal autoscaling based on CPU is not meaningful for a write-rate-limited producer. A fixed size gives operators explicit control. The `01_db_producer` binary already uses `yc.WithMetadataCredentials()` as fallback when `YDB_SA_KEY_FILE` is absent, so the COI VM's service account identity is used automatically.

**Alternatives considered**:
- Autoscale for producer: Rejected — CPU utilisation on a write-loop producer is not a meaningful scale signal; a fixed pool is simpler and sufficient.
- Standalone VM instead of instance group: Rejected — spec assumption says "compute instance group consistent with workers pattern".

---

## Q5: Which variables are new (additive) vs changed?

**Decision**: Add three new root-level variables (`producer_size`, `producer_rate`, `producer_parallelism`) with sensible defaults. No existing variables are renamed or removed.

| Variable | Default | Purpose |
| -------- | ------- | ------- |
| `producer_size` | `1` | Fixed size of the producer instance group |
| `producer_rate` | `100` | Tasks/second (maps to `--rate` flag if applicable; note: `01_db_producer` does not have a `--rate` flag — it runs at full speed; this variable is reserved for future use and can be omitted from docker-compose initially) |
| `producer_parallelism` | `10` | Maps to `--parallelism` flag in `01_db_producer` |

**Rationale**: Keeping new variables additive ensures existing `terraform.tfvars` files continue to work without modification (SC-006).

---

## Q6: Does the `migrations` image build belong in `workers` or a new location?

**Decision**: Keep `migrations` image build in the `workers` module. The migrations image is primarily used by the operator to run schema migrations — it is a sibling concern to the worker deployment, not the producer.

**Rationale**: Avoids creating a fourth module for a single resource. Migrations are conceptually tied to the workers deploy phase (you run migrations before scaling workers).

---

## Q7: How are `subnet_ids` passed to workers and producer?

**Decision**: The `db` module outputs `subnet_ids = [for s in yandex_vpc_subnet.main : s.id]` as a `list(string)`. Workers and producer receive this as `var.subnet_ids`.

**Rationale**: Subnets are created by the `db` module (they depend on the VPC). Workers and producer need subnet IDs for `network_interface.subnet_ids`. Passing the list as an output is the cleanest approach.
