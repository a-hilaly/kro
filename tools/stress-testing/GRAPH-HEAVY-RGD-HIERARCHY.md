# Graph-Heavy Stress Hierarchy Proposal

This document describes the proposed `graph-heavy` stress-test hierarchy
for KRO.

Current profiles implemented in the stress-testing tool:
- `graph-heavy`
  - `10` total RGDs
  - `70` leaf resources per parent instance
  - `10` total instance CRs per parent instance
  - `80` total objects per parent instance including instances
- `graph-heavy-50`
  - `50` total RGDs
  - `256` leaf resources per parent instance
  - `50` total instance CRs per parent instance
  - `306` total objects per parent instance including instances

`graph-heavy-50` is a real hierarchy, not a flat fanout:
- root layer: `5` child-instance resources
- domain layer: `19` intermediate child-instance resources
- group layer: `25` intermediate child-instance resources
- leaf layer: `25` resource-producing RGDs

For `graph-heavy-50`, the current `1,000,000` total-object projection is:
- `3268` parent instances
- `836608` leaf resources
- `163400` total instance CRs
- `1000008` total objects including instances

Per parent instance for `graph-heavy-50`:
- `256` leaf resources
- `50` instance CRs
- `306` total objects including instances

## CRD Setup

The richer rendered profiles include ACK CRDs that may not already be installed
in the cluster. `graph-heavy-50` setup will fail fast if these CRDs are not
present, so this is a required prereq before running the hierarchy setup
command.

The stress tool now has a generic CRD installer:

```bash
go run ./tools/stress-testing/cmd/krostress stress rgd setup-crds \
  --url <raw-crd-yaml-url> \
  --url <raw-crd-yaml-url>
```

For the current `graph-heavy-50` profile, the richer ACK shape needs these
CRDs:
- `ec2.services.k8s.aws_subnets.yaml`
- `ec2.services.k8s.aws_securitygroups.yaml`
- `ec2.services.k8s.aws_routetables.yaml`
- `dynamodb.services.k8s.aws_tables.yaml`
- `rds.services.k8s.aws_dbsubnetgroups.yaml`
- `rds.services.k8s.aws_dbinstances.yaml`

Using upstream ACK raw GitHub URLs, that looks like:

```bash
EC2_REPO="https://raw.githubusercontent.com/aws-controllers-k8s/\
ec2-controller/main"
DDB_REPO="https://raw.githubusercontent.com/aws-controllers-k8s/\
dynamodb-controller/main"
RDS_REPO="https://raw.githubusercontent.com/aws-controllers-k8s/\
rds-controller/main"

go run ./tools/stress-testing/cmd/krostress stress rgd setup-crds \
  --url "${EC2_REPO}/config/crd/bases/ec2.services.k8s.aws_subnets.yaml" \
  --url "${EC2_REPO}/config/crd/bases/\
ec2.services.k8s.aws_securitygroups.yaml" \
  --url "${EC2_REPO}/config/crd/bases/ec2.services.k8s.aws_routetables.yaml" \
  --url "${DDB_REPO}/config/crd/bases/dynamodb.services.k8s.aws_tables.yaml" \
  --url "${RDS_REPO}/config/crd/bases/\
rds.services.k8s.aws_dbsubnetgroups.yaml" \
  --url "${RDS_REPO}/config/crd/bases/rds.services.k8s.aws_dbinstances.yaml"
```

After that, hierarchy setup can be run with:

```bash
go run ./tools/stress-testing/cmd/krostress stress rgd setup \
  --hierarchy graph-heavy-50
```

## Staged Run Plan

The `1,000,000`-object run should be executed in stages, not in one jump.
Each stage should complete, settle, and produce a consistent artifact bundle
before the next stage starts.

### Stage Counts

| Stage | Parents | Leaf objs | Inst CRs | Total objs |
|-------|---------|-----------|----------|------------|
| `0` | `0` | `0` | `0` | `0` |
| `1` | `100` | `25600` | `5000` | `30600` |
| `2` | `250` | `64000` | `12500` | `76500` |
| `3` | `500` | `128000` | `25000` | `153000` |
| `4` | `1000` | `256000` | `50000` | `306000` |
| `5` | `2000` | `512000` | `100000` | `612000` |
| `6` | `3268` | `836608` | `163400` | `1000008` |

### Stage Notes

| Stage | Dir | Notes |
|-------|-----|-------|
| `0` | `stage-0-setup/` | Install CRDs and wait `50/50` RGDs active |
| `1` | `stage-1-030600-objects/` | Smoke stage, verify metrics |
| `2` | `stage-2-076500-objects/` | Verify watch metrics and queues |
| `3` | `stage-3-153000-objects/` | Check churn and cache shape |
| `4` | `stage-4-306000-objects/` | Validate mid-scale behavior |
| `5` | `stage-5-612000-objects/` | Pre-final stability check |
| `6` | `stage-6-1000008-objects/` | Final million-object stage |

### Stage Gate

Do not advance to the next stage until:
- all expected parent instances exist
- the top-level parent instances are `ACTIVE`
- controller queues drain back to the expected idle level
- controller restart count is unchanged
- the stage artifact directory is complete

If a stage fails:
- stop the ramp
- capture the hold-state profiles
- mark the stage directory with a `-failed` suffix
- write the failure reason and exact stop time into `README.md`

## Artifact Layout

Each stage should write into a dedicated directory under:

```text
./tools/stress-testing/results/
```

Recommended naming:

```text
./stage-<n>-<total-objects>-objects/
```

Example:

```text
./stage-4-306000-objects/
```

Each stage directory should contain:
- `README.md`
  - stage number
  - target counts
  - realized counts
  - git commit
  - image tag and digest
  - start time and end time
  - pass/fail notes
- `summary.json`
  - final rolled-up metrics for the stage
- `observation.json`
  - raw sampled timeseries for the stage
- `metrics-start.prom`
  - `/metrics` snapshot before the ramp
- `metrics-end.prom`
  - `/metrics` snapshot after settle
- `pprof-start/`
  - snapshot before adding parent instances
- `pprof-mid/`
  - snapshot near peak queue depth or peak heap
- `pprof-hold/`
  - snapshot after queues drain and the system settles
- `report.html`
  - if the run path emits an HTML report
- `notes.md`
  - operator notes, surprises, and actions before the next stage

Suggested snapshot timing per stage:
- `pprof-start`
  - immediately before creating more parent instances
- `pprof-mid`
  - once the queue is materially loaded or around the expected peak
- `pprof-hold`
  - after the queue returns to `0` or the steady baseline for that stage

Suggested operational data to capture in each `README.md`:
- parent instance target
- realized parent instance count
- realized child instance count
- realized leaf resource count
- total object count including instances
- peak CPU
- peak working set
- peak RSS
- peak heap in use
- peak goroutines
- peak dynamic queue depth
- peak RGD queue depth
- controller restarts during stage
- whether any rollback, deregister, or stale-registration metrics moved

Goals:
- model a much heavier graph than `low`, `medium`, or `high`
- keep Kubernetes side effects low
- put the stress on KRO graph build, CEL wiring, status projection,
  dynamic controller fanout, and chained RGD reconciliation
- keep all `Deployment` objects at `replicas: 0`
- avoid `Service`, `Ingress`, `NetworkPolicy`,
  `PodDisruptionBudget`, `Lease`, `Job`, and `CronJob`

Assumptions:
- total hierarchy size is `10` RGDs
- one child RGD carries `20` resources
- all other child RGDs carry `5-10` resources
- ACK-heavy children are create-only and do not rely on a controller
  behind the scenes
- ACK-heavy children use synthetic IDs derived from schema inputs
  instead of live status
- the richer ACK variant assumes these CRDs are installed:
  - `subnets.ec2.services.k8s.aws`
  - `securitygroups.ec2.services.k8s.aws`
  - `routetables.ec2.services.k8s.aws`
  - `tables.dynamodb.services.k8s.aws`
  - `dbsubnetgroups.rds.services.k8s.aws`
  - `dbinstances.rds.services.k8s.aws`
- if those CRDs are not installed, `graph-heavy-ack-network` and
  `graph-heavy-ack-data` should fall back to repeated `Bucket` and
  `VPC` objects

## Hierarchy

- `graph-heavy-stack`
  - parent RGD
  - resource count: `9`
  - creates one child instance for each of the `9` child RGDs below

- `graph-heavy-platform-core`
  - shared platform layer
  - resource count: `20`

- `graph-heavy-web`
  - web workload slice
  - resource count: `6`

- `graph-heavy-api`
  - api workload slice
  - resource count: `6`

- `graph-heavy-worker`
  - worker workload slice
  - resource count: `6`

- `graph-heavy-auth`
  - auth and token material
  - resource count: `6`

- `graph-heavy-rbac`
  - explicit RBAC-only layer
  - resource count: `6`

- `graph-heavy-config`
  - config and secret-heavy layer
  - resource count: `6`

- `graph-heavy-ack-network`
  - inert ACK network CRDs
  - resource count: `7`

- `graph-heavy-ack-data`
  - inert ACK data CRDs
  - resource count: `7`

## Shared Conventions

- every `Deployment` uses `spec.replicas: ${schema.spec.replicas}`
  with schema default `0`
- native children expose stable names and references through status
- ACK children expose synthetic IDs and metadata-derived names through status
- ACK children should use cheap readiness only:
  - `${resource.metadata.generation > 0}`
  - or `${resource.metadata.uid != ""}`
- ACK children should not use fields such as `${resource.status.*}`
  for correctness
- parent-to-child wiring should pass shared fields:
  - `name`
  - `namespace`
  - `owner`
  - `tier`
  - `replicas`
  - images or image tags
  - feature toggles
  - synthetic IDs for ACK wiring

## `graph-heavy-stack`

- purpose:
  - top-level composer
  - fans out into all child RGDs
  - aggregates child status back into one summary view
- resources:
  - `platformCore`
  - `web`
  - `api`
  - `worker`
  - `auth`
  - `rbac`
  - `config`
  - `ackNetwork`
  - `ackData`
- wiring:
  - passes common schema fields to every child
  - passes synthetic infra fields to ACK-heavy children:
    - `fakeVpcAID`
    - `fakeVpcBID`
    - `fakeSubnetAID`
    - `fakeSubnetBID`
    - `fakeSecurityGroupID`
    - `fakeRouteTableID`
    - `assetsBucketName`
    - `logsBucketName`
    - `eventsTableName`
    - `sessionsTableName`
    - `dbSubnetGroupName`
    - `dbInstanceIdentifier`
- status fields:
  - `children.platformCore`
  - `children.web`
  - `children.api`
  - `children.worker`
  - `children.auth`
  - `children.rbac`
  - `children.config`
  - `children.ackNetwork`
  - `children.ackData`
  - `deployments.web`
  - `deployments.api`
  - `deployments.worker`
  - `serviceAccounts.web`
  - `serviceAccounts.api`
  - `serviceAccounts.worker`
  - `infra.assetsBucket`
  - `infra.logsBucket`
  - `infra.vpcAID`
  - `infra.vpcBID`
  - `infra.eventsTable`
  - `infra.sessionsTable`
  - `infra.dbInstanceIdentifier`

## `graph-heavy-platform-core`

- purpose:
  - large shared child with the heaviest native object mix
  - main `20`-resource stress anchor
- resources:
  - `baseConfig`
  - `featureFlags`
  - `runtimeConfig`
  - `policyConfig`
  - `platformSecret`
  - `signingSecret`
  - `tlsSecret`
  - `sessionSecret`
  - `platformServiceAccount`
  - `webServiceAccount`
  - `apiServiceAccount`
  - `platformRole`
  - `webRole`
  - `apiRole`
  - `platformBinding`
  - `webBinding`
  - `apiBinding`
  - `assetsBucket`
  - `logsBucket`
  - `sharedVpc`
- wiring:
  - `RoleBinding -> Role + ServiceAccount`
  - config and secrets reference `schema.spec.name`, `owner`, and `tier`
  - bucket and vpc names come from deterministic schema-based strings
- status fields:
  - `config.baseConfigName`
  - `config.featureFlagsName`
  - `config.runtimeConfigName`
  - `config.policyConfigName`
  - `secrets.platformSecretName`
  - `secrets.signingSecretName`
  - `secrets.tlsSecretName`
  - `secrets.sessionSecretName`
  - `serviceAccounts.platform`
  - `serviceAccounts.web`
  - `serviceAccounts.api`
  - `roles.platform`
  - `roles.web`
  - `roles.api`
  - `infra.assetsBucketName`
  - `infra.logsBucketName`
  - `infra.sharedVpcName`
  - `infra.sharedVpcID`

## `graph-heavy-web`

- purpose:
  - web-facing workload slice
- resources:
  - `webDeployment`
  - `webConfig`
  - `webSecret`
  - `webServiceAccount`
  - `webRole`
  - `webBinding`
- wiring:
  - `webDeployment.serviceAccountName -> webServiceAccount`
  - `webDeployment.envFrom -> webConfig + webSecret`
  - `webBinding -> webRole + webServiceAccount`
- status fields:
  - `deploymentName`
  - `deploymentGeneration`
  - `serviceAccountName`
  - `configName`
  - `secretName`
  - `roleName`
  - `roleBindingName`

## `graph-heavy-api`

- purpose:
  - api workload slice
- resources:
  - `apiDeployment`
  - `apiConfig`
  - `apiSecret`
  - `apiServiceAccount`
  - `apiRole`
  - `apiBinding`
- wiring:
  - `apiDeployment.serviceAccountName -> apiServiceAccount`
  - `apiDeployment.envFrom -> apiConfig + apiSecret`
  - `apiBinding -> apiRole + apiServiceAccount`
- status fields:
  - `deploymentName`
  - `deploymentGeneration`
  - `serviceAccountName`
  - `configName`
  - `secretName`
  - `roleName`
  - `roleBindingName`

## `graph-heavy-worker`

- purpose:
  - worker workload slice
- resources:
  - `workerDeployment`
  - `workerConfig`
  - `workerSecret`
  - `workerServiceAccount`
  - `workerRole`
  - `workerBinding`
- wiring:
  - `workerDeployment.serviceAccountName -> workerServiceAccount`
  - `workerDeployment.envFrom -> workerConfig + workerSecret`
  - `workerBinding -> workerRole + workerServiceAccount`
- status fields:
  - `deploymentName`
  - `deploymentGeneration`
  - `serviceAccountName`
  - `configName`
  - `secretName`
  - `roleName`
  - `roleBindingName`

## `graph-heavy-auth`

- purpose:
  - auth and token material
- resources:
  - `authConfig`
  - `publicTLSSecret`
  - `privateTLSSecret`
  - `tokenSignerSecret`
  - `authServiceAccount`
  - `authBinding`
- wiring:
  - auth secrets derive names and annotations from shared inputs
  - `authBinding` binds a built-in or generated role to `authServiceAccount`
- status fields:
  - `configName`
  - `publicTLSSecretName`
  - `privateTLSSecretName`
  - `tokenSignerSecretName`
  - `serviceAccountName`
  - `roleBindingName`

## `graph-heavy-rbac`

- purpose:
  - isolate RBAC object churn into its own child
- resources:
  - `readerRole`
  - `writerRole`
  - `auditorRole`
  - `readerBinding`
  - `writerBinding`
  - `auditorBinding`
- wiring:
  - bindings refer to shared service-account names passed from parent
  - role rules can pin `resourceNames` to child config and secret names
- status fields:
  - `roles.reader`
  - `roles.writer`
  - `roles.auditor`
  - `bindings.reader`
  - `bindings.writer`
  - `bindings.auditor`

## `graph-heavy-config`

- purpose:
  - config-heavy child with no workload controller
- resources:
  - `appConfig`
  - `featureConfig`
  - `runtimeConfig`
  - `appSecret`
  - `tlsSecret`
  - `sessionSecret`
- wiring:
  - values reference parent schema fields and selected child names
  - no status-driven readiness needed
- status fields:
  - `configs.app`
  - `configs.feature`
  - `configs.runtime`
  - `secrets.app`
  - `secrets.tls`
  - `secrets.session`

## `graph-heavy-ack-network`

- purpose:
  - inert ACK network objects
  - exercises non-native schemas without relying on cloud controllers
- resources:
  - `vpcA`
  - `vpcB`
  - `subnetA`
  - `subnetB`
  - `securityGroup`
  - `routeTable`
  - `routeAssociation`
- wiring:
  - `subnetA.spec.vpcID = ${schema.spec.fakeVpcAID}`
  - `subnetB.spec.vpcID = ${schema.spec.fakeVpcBID}`
  - `securityGroup.spec.vpcID = ${schema.spec.fakeVpcAID}`
  - `routeTable.spec.vpcID = ${schema.spec.fakeVpcAID}`
  - `routeAssociation` uses synthetic subnet and route-table IDs
- status fields:
  - `vpcAName`
  - `vpcBName`
  - `vpcAID`
  - `vpcBID`
  - `subnetAID`
  - `subnetBID`
  - `securityGroupID`
  - `routeTableID`

## `graph-heavy-ack-data`

- purpose:
  - inert ACK data-plane objects
  - exercises additional CRD schemas and CEL wiring
- resources:
  - `assetsBucket`
  - `logsBucket`
  - `eventsTable`
  - `sessionsTable`
  - `dbSubnetGroup`
  - `dbInstance`
  - `dbCredentialsSecret`
- wiring:
  - bucket names come from deterministic schema-based strings
  - table names come from deterministic schema-based strings
  - `dbSubnetGroup.spec.subnetIDs` uses synthetic subnet IDs from parent
  - `dbInstance.spec.dbSubnetGroupName` uses the same deterministic
    subnet group name
  - `dbCredentialsSecret` is native and gives the child one native
    dependency too
- status fields:
  - `assetsBucketName`
  - `logsBucketName`
  - `eventsTableName`
  - `sessionsTableName`
  - `dbSubnetGroupName`
  - `dbInstanceIdentifier`
  - `dbCredentialsSecretName`

## Why This Shape

- parent chaining is exercised heavily
- one child is large enough to act as the primary builder/CEL stressor
- the rest stay in the `5-10` resource range for fanout realism
- the hierarchy mixes:
  - native Kubernetes resources
  - generated child RGD instances
  - ACK CRDs with synthetic wiring
- there are no pods, jobs, services, or ingresses that would
  materially shift load away from KRO itself

## Current Implementation Status

- `graph-heavy` render and plan support are implemented
- `graph-heavy-50` render and plan support are implemented
- the richer ACK objects in these rendered profiles still require the
  corresponding CRDs to be installed before they can be applied
- an optional `graph-heavy-ack-lite` fallback is still a useful follow-up
  if we want apply-safe rendering on clusters that only have `Bucket` and
  `VPC`
