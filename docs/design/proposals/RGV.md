# ResourceGraphRevision (RGV) Design

## Summary

This document proposes **ResourceGraphRevision (RGV)**, a primitive for tracking changes to ResourceGraphDefinitions and their instances over time. RGV enables rollback, propagation control, audit trails, and safe change management for kro.

## Motivation

### The Problem

Today, when an RGD or instance is updated:
1. Changes propagate immediately to all affected resources
2. There's no way to rollback to a previous state
3. No audit trail of what changed when
4. No mechanism to control the pace of change propagation
5. No way to pin instances to specific RGD versions

This makes kro unsuitable for production environments where:
- Changes need controlled rollout (canary, progressive)
- Rollback must be fast and reliable
- Compliance requires audit trails
- Different environments need different versions

### Prior Art

| System | Revision Mechanism | Storage | Rollback |
|--------|-------------------|---------|----------|
| [Kubernetes StatefulSet](https://medium.com/@dhanielluis/understanding-kubernetes-statefulset-revision-history-management-58bf1e433f84) | ControllerRevision CRD | Immutable snapshots | `kubectl rollout undo` |
| [Argo Rollouts](https://argo-rollouts.readthedocs.io/en/stable/features/rollback/) | ReplicaSets | Revision history | `kubectl argo rollouts undo` |
| [Crossplane](https://docs.crossplane.io/v1.20/concepts/composition-revisions/) | PackageRevision CRD | Active/Inactive | Manual activation |
| [Helm](https://helm.sh/docs/helm/helm_rollback/) | Secrets per revision | Encoded state | `helm rollback` |
| [Flux HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/) | Helm + GitOps | Git history | `git revert` |

**Key insights from prior art:**
- Immutable snapshots are essential for reliable rollback
- Monotonically increasing revision numbers provide ordering
- `revisionHistoryLimit` prevents unbounded storage growth
- Separation of "what to deploy" from "when to deploy" enables safe rollout

## Design Overview

### Two-Level Revision Model

kro needs to track changes at two levels:

```
┌─────────────────────────────────────────────────────────────────┐
│                  ResourceGraphDefinition                         │
│                                                                 │
│  Revision 1 ──▶ Revision 2 ──▶ Revision 3 (current)            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ instances reference RGD revision
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Instance                                  │
│                                                                 │
│  Input Rev 1 ──▶ Input Rev 2 ──▶ Input Rev 3 (current)         │
│  (RGD rev 1)     (RGD rev 2)     (RGD rev 3)                   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

1. **RGD Revisions**: Track changes to the ResourceGraphDefinition itself
   - Triggered when `spec.resources` or `spec.schema` changes
   - Stored as immutable `ResourceGraphDefinitionRevision` objects
   - Enables "rollback the platform API to last week"

2. **Instance Input Revisions**: Track the combination of RGD revision + instance spec
   - Triggered when instance spec changes OR when RGD revision changes
   - Stored in instance status or as separate objects
   - Enables propagation control and instance-level rollback

### New CRDs

#### ResourceGraphDefinitionRevision

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinitionRevision
metadata:
  name: application-rev-3                    # {rgd-name}-rev-{number}
  namespace: default
  ownerReferences:
    - apiVersion: kro.run/v1alpha1
      kind: ResourceGraphDefinition
      name: application
      controller: true
  labels:
    kro.run/rgd-name: application
    kro.run/revision-number: "3"
spec:
  # Immutable snapshot of RGD spec at this revision
  revision: 3                                # Monotonically increasing
  schema:
    kind: Application
    apiVersion: v1alpha1
    spec: { ... }                            # Full schema snapshot
    status: { ... }
  resources:
    - id: deployment
      template: { ... }                      # Full template snapshot
      readyWhen: [...]
      includeWhen: [...]
    - id: service
      template: { ... }
status:
  # Hash of the spec for quick comparison
  specHash: "abc123def456"
  # When this revision was created
  createdAt: "2025-01-15T10:30:00Z"
  # Whether this revision has been superseded
  superseded: false
  # Number of instances currently using this revision
  instanceCount: 47
```

#### ResourceGraphInputRevision (Optional - for fine-grained tracking)

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphInputRevision
metadata:
  name: my-app-input-rev-12                  # {instance-name}-input-rev-{number}
  namespace: default
  ownerReferences:
    - apiVersion: apps.example.com/v1alpha1  # Generated CRD
      kind: Application
      name: my-app
      controller: true
  labels:
    kro.run/instance-name: my-app
    kro.run/input-revision: "12"
    kro.run/rgd-revision: "3"
spec:
  inputRevision: 12                          # Monotonically increasing per-instance
  rgdRevision: 3                             # Which RGD revision this is based on
  rgdRevisionRef:
    name: application-rev-3
  instanceSpec:                              # Snapshot of instance spec
    name: my-app
    replicas: 5
    image: nginx:1.21
status:
  specHash: "xyz789"
  createdAt: "2025-01-15T10:35:00Z"
  # Propagation status
  propagationStatus: Completed               # Pending | InProgress | Completed | Failed
  resourcesAtRevision: 5                     # How many resources are at this revision
  totalResources: 5
```

### Resource Annotations

Managed resources are annotated with their revision:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app-deployment
  annotations:
    kro.run/rgd-revision: "3"                # RGD revision that created this
    kro.run/input-revision: "12"             # Instance input revision
    kro.run/instance-name: my-app
    kro.run/instance-namespace: default
    kro.run/resource-id: deployment
```

## API Changes

### ResourceGraphDefinition

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: application
spec:
  # Existing fields...
  schema: { ... }
  resources: [ ... ]

  # NEW: Revision management
  revisionManagement:
    # How many old revisions to keep (default: 10)
    historyLimit: 10

    # How to handle RGD updates
    updatePolicy:
      # Automatic: Create revision immediately on change
      # Manual: Require annotation to create new revision
      type: Automatic                        # Automatic | Manual

status:
  # Existing fields...
  conditions: [ ... ]

  # NEW: Revision status
  currentRevision: 3
  currentRevisionName: application-rev-3
  currentRevisionHash: "abc123def456"
  observedGeneration: 3

  # Revision history summary
  revisionHistory:
    - revision: 3
      createdAt: "2025-01-15T10:30:00Z"
      current: true
      instanceCount: 47
    - revision: 2
      createdAt: "2025-01-14T09:00:00Z"
      current: false
      instanceCount: 3                       # Instances still on old revision
    - revision: 1
      createdAt: "2025-01-10T08:00:00Z"
      current: false
      instanceCount: 0
```

### Instance (Generated CRD)

```yaml
apiVersion: apps.example.com/v1alpha1
kind: Application
metadata:
  name: my-app
  annotations:
    # NEW: Pin to specific RGD revision (optional)
    kro.run/rgd-revision: "2"                # Pin to revision 2

    # NEW: Control update behavior
    kro.run/update-policy: "Manual"          # Auto | Manual | Paused
spec:
  name: my-app
  replicas: 5
  image: nginx:1.21

status:
  # Existing fields...
  conditions: [ ... ]

  # NEW: Revision tracking
  currentInputRevision: 12
  currentRGDRevision: 3
  observedInputRevision: 12
  observedRGDRevision: 3

  # NEW: Propagation status (for KREP-006)
  propagation:
    status: Completed                        # Pending | InProgress | Completed | Blocked
    targetInputRevision: 12
    resourcesAtTarget: 5
    totalResources: 5
    lastTransitionTime: "2025-01-15T10:36:00Z"

  # NEW: Revision history summary
  inputRevisionHistory:
    - revision: 12
      rgdRevision: 3
      createdAt: "2025-01-15T10:35:00Z"
      status: Completed
    - revision: 11
      rgdRevision: 3
      createdAt: "2025-01-15T09:00:00Z"
      status: Superseded
```

## Behavior

### Revision Creation

#### RGD Revision Creation

```
RGD Created/Updated
        │
        ▼
┌───────────────────────────────────────────────────────┐
│  Compare spec hash with current revision              │
│                                                       │
│  If different:                                        │
│    1. Create new ResourceGraphDefinitionRevision      │
│    2. Increment revision number                       │
│    3. Update RGD status.currentRevision               │
│    4. Mark old revision as superseded                 │
│    5. Trigger instance reconciliation                 │
│                                                       │
│  If same:                                             │
│    No new revision needed                             │
└───────────────────────────────────────────────────────┘
```

#### Input Revision Creation

```
Instance Created/Updated OR RGD Revision Changed
        │
        ▼
┌───────────────────────────────────────────────────────┐
│  Compute input hash = hash(instance.spec + rgdRev)   │
│                                                       │
│  If different from current:                           │
│    1. Create new input revision (inline or CRD)      │
│    2. Increment input revision number                 │
│    3. Begin propagation to resources                  │
│    4. Annotate resources with new revision            │
│                                                       │
│  If same:                                             │
│    No new revision needed                             │
└───────────────────────────────────────────────────────┘
```

### Rollback

#### RGD Rollback

Rollback the RGD to a previous revision, affecting all instances:

```bash
# CLI (future)
kro rollback rgd application --to-revision=2

# Or via annotation
kubectl annotate rgd application kro.run/rollback-to-revision=2
```

This:
1. Copies spec from `application-rev-2` back to RGD
2. Creates new revision (e.g., revision 4) with that content
3. Instances reconcile to new revision

Note: Rollback creates a *new* revision with old content, preserving history.

#### Instance Rollback

Rollback a single instance to a previous input revision:

```bash
# CLI (future)
kro rollback instance application/my-app --to-input-revision=10

# Or via annotation
kubectl annotate application my-app kro.run/rollback-to-input-revision=10
```

This:
1. Retrieves instance spec from input revision 10
2. Patches instance with that spec
3. Creates new input revision
4. Resources reconcile to new state

### Revision Pinning

Pin an instance to a specific RGD revision:

```yaml
apiVersion: apps.example.com/v1alpha1
kind: Application
metadata:
  name: my-app-production
  annotations:
    # Stay on RGD revision 2, ignore newer revisions
    kro.run/rgd-revision: "2"
```

Use cases:
- Keep production on stable version while testing new RGD in staging
- Gradual rollout of RGD changes
- Emergency freeze during incidents

### History Pruning

Old revisions are pruned based on `revisionHistoryLimit`:

```go
func pruneRevisions(rgd *RGD, revisions []Revision) {
    // Keep revisions that are:
    // 1. Current revision
    // 2. Referenced by any instance (pinned or in-progress)
    // 3. Within historyLimit of current revision

    for _, rev := range revisions {
        if rev.IsCurrent() || rev.HasActiveInstances() {
            continue // Keep
        }
        if rgd.Status.CurrentRevision - rev.Number <= rgd.Spec.HistoryLimit {
            continue // Keep
        }
        delete(rev) // Prune
    }
}
```

## Integration with KREP-006 (Propagation Control)

ResourceGraphRevision is the foundation for propagation control:

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: application
spec:
  # From KREP-006
  propagateWhen:
    - ${exponentiallyUpdated(instances, each)}
```

The `exponentiallyUpdated` function checks:
```cel
// How many instances have resources at the current RGD revision?
size(instances.filter(i,
    i.status.observedRGDRevision == rgd.status.currentRevision
)) >= exponentialTarget(each.index)
```

Resources are considered "at revision" when their `kro.run/input-revision` annotation matches the target.

## Storage Considerations

### Revision Object Size

Each `ResourceGraphDefinitionRevision` contains a full snapshot of the RGD spec. For large RGDs:

- **Typical size**: 10-50 KB per revision
- **With 10 revisions**: 100-500 KB total
- **etcd limit**: 1.5 MB per object (not a concern)

### Alternative: Patch-Based Storage

Instead of full snapshots, store patches:

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinitionRevision
spec:
  revision: 3
  # Only store the diff from previous revision
  patchFrom: application-rev-2
  patch:
    type: JSONPatch
    operations:
      - op: replace
        path: /spec/resources/0/template/spec/replicas
        value: 5
```

**Trade-offs:**
- Smaller storage
- Requires traversing history to reconstruct state
- More complex implementation
- Kubernetes ControllerRevision uses full snapshots; we should too

**Recommendation**: Use full snapshots for simplicity and reliability. Storage is cheap; complexity is expensive.

### Input Revision Storage

Two options:

1. **Inline in instance status** (simpler):
   ```yaml
   status:
     inputRevisionHistory:
       - revision: 12
         specHash: "xyz789"
         createdAt: "..."
   ```

2. **Separate CRD** (more powerful):
   - Enables querying across instances
   - Larger storage overhead
   - More flexible for advanced features

**Recommendation**: Start with inline storage, add CRD later if needed.

## CLI Commands

```bash
# List RGD revisions
kro revisions list rgd/application
# REVISION  CREATED              INSTANCES  STATUS
# 3         2025-01-15 10:30:00  47         Current
# 2         2025-01-14 09:00:00  3          Active
# 1         2025-01-10 08:00:00  0          Prunable

# Show revision diff
kro revisions diff rgd/application --from=2 --to=3
# --- revision 2
# +++ revision 3
# @@ spec.resources.0.template.spec @@
# - replicas: 3
# + replicas: 5

# Rollback RGD
kro rollback rgd/application --to-revision=2
# Rolling back to revision 2...
# Created revision 4 (content from revision 2)
# 47 instances will reconcile to new revision

# Show instance revision history
kro revisions list application/my-app
# INPUT-REV  RGD-REV  CREATED              STATUS
# 12         3        2025-01-15 10:35:00  Completed
# 11         3        2025-01-15 09:00:00  Superseded
# 10         2        2025-01-14 10:00:00  Superseded

# Rollback instance
kro rollback application/my-app --to-input-revision=10
# Rolling back to input revision 10...
# Instance will reconcile to RGD revision 2

# Pin instance to RGD revision
kro pin application/my-app --rgd-revision=2
# Instance pinned to RGD revision 2
# Will not receive updates from newer RGD revisions

# Unpin instance
kro unpin application/my-app
# Instance unpinned, will use latest RGD revision
```

## Implementation Phases

### Phase 1: RGD Revision Tracking

- Create `ResourceGraphDefinitionRevision` CRD
- Generate revisions on RGD changes
- Track `currentRevision` in RGD status
- Annotate managed CRDs with `kro.run/rgd-revision`
- Implement history pruning
- Basic CLI: `kro revisions list`

### Phase 2: Instance Input Tracking

- Track input revisions in instance status
- Annotate resources with `kro.run/input-revision`
- Add `propagation` status to instances
- Detect when resources are "at revision"

### Phase 3: Rollback

- RGD rollback via annotation
- Instance rollback via annotation
- CLI: `kro rollback`
- Rollback creates new revision (preserves history)

### Phase 4: Revision Pinning

- Instance pinning via annotation
- CLI: `kro pin` / `kro unpin`
- Respect pins during RGD updates

### Phase 5: Propagation Control Integration

- Integrate with KREP-006 `propagateWhen`
- Support `exponentiallyUpdated` / `linearlyUpdated`
- Handle overlapping propagations

## Open Questions

1. **Should input revisions be a separate CRD or inline in status?**
   - Inline is simpler but limits queryability
   - Separate CRD adds overhead but enables advanced features

2. **How do collections interact with revisions?**
   - Each collection item gets same revision annotation
   - Or each item could have its own revision tracking?

3. **What happens to in-flight resources during rollback?**
   - Cancel in-progress operations?
   - Let them complete then rollback?

4. **Should rollback be instant or respect propagateWhen?**
   - Instant rollback for emergencies
   - Controlled rollback for normal operations
   - Configurable per-rollback?

5. **How do decorators interact with revisions?**
   - Decorator changes create new revisions
   - But decorated resources aren't owned by instances
   - Need separate tracking?

## References

- [Kubernetes ControllerRevision](https://medium.com/@dhanielluis/understanding-kubernetes-statefulset-revision-history-management-58bf1e433f84)
- [Argo Rollouts Rollback](https://argo-rollouts.readthedocs.io/en/stable/features/rollback/)
- [Crossplane Composition Revisions](https://docs.crossplane.io/v1.20/concepts/composition-revisions/)
- [Helm Rollback](https://helm.sh/docs/helm/helm_rollback/)
- [Flux HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/)
- [KREP-006: Propagation Control](https://github.com/kubernetes-sigs/kro/pull/861)
