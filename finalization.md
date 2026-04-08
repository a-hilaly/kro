# KREP-TBD: Protocol-Driven Finalization with `onDelete`

## Summary

KRO needs a first-class deletion model for resources that cannot be safely
cleaned up by unordered garbage collection. Some graphs need teardown ordering,
pre-delete actions such as final snapshots or verification jobs, and explicit
control over when a resource is considered safe to remove.

This proposal introduces `onDelete` as a node-attached protocol. An `onDelete`
protocol is a graph with its own planner that runs before the parent node's
resources are deleted. Instance deletion is driven by a single finalizer on the
instance CR, a reverse-topological walk of the main graph, and explicit deletion
of child resources. If an `onDelete` protocol fails, the instance stays in
`DELETING` and retries on the next reconcile.

## Problem Statement

Deletion is not just "apply the graph in reverse." Real systems often need
destructive actions to be preceded by workflow:

- databases need final snapshots
- external systems need deregistration or drain steps
- teardown must respect dependency order
- cleanup may require creating temporary helper resources before deletion

Kubernetes owner references and generic garbage collection do not solve that
problem well for KRO:

- cross-namespace resources cannot use owner references
- shared ownership and future reference-counting are incompatible with
  single-owner garbage collection
- garbage collection does not model pre-delete workflows
- child-level finalizers make lifecycle ownership diffuse and harder to reason
  about

KRO therefore needs a deletion model in which the orchestrator owns ordering,
cleanup, and finalization explicitly.

## Proposal

### Overview

`onDelete` is a lifecycle protocol attached to a node. A protocol is a graph
with the same node, kernel, and trait primitives as the main graph, but with its
own execution strategy and trigger. `onDelete` runs when the instance is being
deleted and the orchestrator reaches the owning node during reverse-order
teardown.

Conceptually:

```yaml
resources:
  - id: database
    # ...
    onDelete:
      strategy: serial
      resources:
        - id: finalSnapshot
          # ...
        - id: verify
          dependsOn: [finalSnapshot]
          # ...
```

When the protocol completes successfully, the orchestrator deletes the parent
node's resources and continues walking the graph. When the protocol fails, the
instance remains in `DELETING` and the orchestrator retries on the next
reconcile.

### Finalization Model

This proposal uses one Kubernetes finalizer on the instance CR and no kro
finalizers on child resources.

Deletion flow is:

1. Kubernetes sets `deletionTimestamp` on the instance and the instance
   finalizer blocks actual removal.
2. The orchestrator switches to deletion handling.
3. The main graph is walked in reverse topological order.
4. For each node, if `onDelete` is present, the orchestrator executes that
   protocol graph and waits for it to complete.
5. The orchestrator explicitly deletes the parent node's managed resources.
6. Once all nodes are cleaned up, the instance finalizer is removed.
7. Kubernetes completes instance deletion.

This keeps lifecycle ownership centralized: the instance owns the deletion
lifecycle, and the orchestrator owns deletion ordering.

### Protocol Semantics

An `onDelete` protocol is a real graph. It is not a hook callback or an ad-hoc
cleanup block. It has:

- nodes
- dependency ordering
- kernels
- traits
- its own planner

Deletion protocols will usually use a serial planner, but the protocol carries
its own strategy rather than inheriting the main graph's planner.

Protocols are intentionally shallow. Protocol nodes cannot themselves carry
protocols. If a deletion flow grows large enough to need nested failure handling
or richer lifecycle control, that complexity should move into a separate RGD
instance rather than deeper recursion in one controller.

### Scope and Visibility Rules

`onDelete` runs in a degraded context: the graph may already be partially torn
down, and some main-graph resources may no longer exist. To keep protocols
predictable, protocol nodes should only be allowed to reference:

- other nodes inside the same protocol graph
- the parent node that owns the protocol
- the parent's direct dependencies

They should not be allowed to reach arbitrarily across the main graph. The
compiler should enforce this scoping rule.

### Deletion Targeting

Deletion should be based on explicit resource identity, not owner references.
Before issuing deletes, the orchestrator should compute stable targets using the
same node kernel with deletion-specific evaluation options:

- resolve only identity fields such as name and namespace
- normalize namespace routing where needed
- apply any target-routing rewrite needed for multi-cluster or similar cases

For collections, deletion should compare desired identities with observed
membership and delete only the currently owned members. For singleton resources,
deletion should use the stored identity directly.

### Failure Handling During Finalization

If an `onDelete` protocol node fails, the protocol is considered incomplete and
the parent node is not deleted. The instance stays in `DELETING`, status
reflects the incomplete protocol, and the orchestrator retries on the next
reconcile.

This proposal does not introduce a separate nested failure model for deletion
protocols. A failed deletion protocol is retried; it is not itself wrapped in
another protocol.

### Status

Protocol execution state should be reflected on instance status. At minimum,
status should record:

- whether an `onDelete` protocol has started
- whether it is still running, completed, or failed
- timestamps for start and completion
- enough node-level state to make retries observable

This keeps deletion behavior debuggable and restart-safe.

## In Scope

This proposal includes:

- `onDelete` as a node-attached lifecycle protocol
- one finalizer on the instance CR
- reverse-topological deletion of the main graph
- protocol execution before parent-resource deletion
- compiler scoping rules for protocol references
- explicit deletion targeting based on resource identity
- status for protocol execution progress

## Out of Scope

This proposal does not include:

- child-resource finalizers managed by kro
- nested protocols inside protocols
- a separate failure language for deletion protocols
- owner-reference-based deletion as the primary lifecycle mechanism
- reference counting or shared-ownership semantics
- redefining normal reconciliation around deletion

## Testing Strategy

Testing should cover both ordering and failure behavior.

Unit and integration tests should verify that:

- `onDelete` protocols execute before the parent node's resources are deleted
- deletion of the main graph proceeds in reverse topological order
- protocol dependency ordering is respected
- protocol scoping rules are enforced by the compiler
- a failed protocol leaves the instance in `DELETING` and retries later
- the instance finalizer is only removed after all deletion work is complete
- deletion targeting uses stable resource identity rather than owner references

## Discussion and Notes

- This proposal is based on the `Protocols`, `Deletion Protocol Sequence`,
  `Finalizer Placement`, `Protocol Execution`, and `Deletion` sections of
  `/Users/aminehilaly/source/github.com/kubernetes-sigs/kro/designs/rethinking.md`.
- The central design choice is that deletion is orchestrated explicitly by kro,
  not delegated to generic garbage collection.

## Other Solutions Considered

### Owner References and Garbage Collection

The simplest alternative is to rely on owner references and Kubernetes garbage
collection. That is insufficient for KRO because it cannot model pre-delete
workflows, does not handle cross-namespace ownership, and does not give the
orchestrator control over ordering.

### Child-Level Finalizers

Another alternative is to place finalizers on child resources and let each one
manage its own cleanup. That spreads lifecycle control across many objects and
makes graph-level teardown harder to reason about. This proposal keeps the
instance as the single deletion lifecycle anchor.

### Imperative Cleanup Hooks

KRO could also grow custom imperative cleanup callbacks in controller code. That
would solve isolated cases, but it would bypass the graph model and create a
second lifecycle mechanism. The protocol approach keeps cleanup declarative and
composable with the same primitives used elsewhere.
