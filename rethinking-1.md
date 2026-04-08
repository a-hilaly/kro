# kro Primitives — Design Summary

## What We Are Trying To Do

This effort is not "how to improve RGD internals." This effort is "how to define
a reusable concept runtime for kro."

1. RGD is not the primitive; it is one frontend.
2. RGD compiles to smaller execution primitives.
3. New kro features should be additive (new traits/wrappers/passes), not
   reconciler surgery.
4. Multi-cluster proved the model: the graph is mostly the same, only
   targeting/schema resolution changes.
5. Future concepts should be able to compile to the same runtime, even with
   different UX.

RGD compatibility is required. RGD-specific runtime architecture is not the end
state.

## Design Bar

A change is only acceptable if it satisfies all of these:

1. New concepts can be added as new frontends without creating a new runtime.
2. New behavior is additive (new trait/wrapper/lowering rule), not cross-cutting
   reconciler rewrites.
3. Compiler owns dependency inference; runtime executes explicit plans.
4. Cluster IO stays behind one side-effect boundary (orchestrator/backend).
5. RGD user semantics remain compatible during migration.

## Non-Goals

1. Redesigning the RGD user API.
2. Introducing concept-specific runtimes per feature.
3. Moving dependency inference or graph synthesis into runtime execution.
4. Expanding v1 scope beyond parity primitives and required extension points.

## Philosophy

**Tenets:**

1. **Frontend richness, runtime simplicity.** Users write named presets,
   strategy labels, trait names. The compiler decomposes all of it into minimal
   runtime primitives — booleans, functions, CEL expressions. The runtime has no
   enums, no string matching, no awareness of user-facing names.
2. **Additive, not cross-cutting.** New capability = new kernel, new trait, new
   protocol, new planner, or new backend implementation. Never a reconciler
   rewrite.
3. **Compiler owns inference; runtime executes plans.** The compiler infers
   dependencies, validates schemas, resolves imports. The runtime only executes
   what the compiler explicitly produced. No runtime inference.
4. **One mechanism, many policies.** One orchestrator struct, strategy varies
   via planner function. One node implementation, behavior varies via traits.
   Polymorphism lives at the edges, not in the coordinator.
5. **Composition over depth.** Complexity escapes through RGD instances
   (separate controllers), not through deeper nesting or recursive sub-graphs.
   The kernel never gets smarter; you compose more kernels.
6. **Side effects at the boundary.** Only backend talks to the cluster.
   Everything else — kernels, planners, traits — is pure computation or
   coordination.

This design starts from one premise: the right primitives are the ones you stop
being able to break down further. Every concept in this document was
pressure-tested against the same question — can this be built from something we
already have? If yes, it's not a primitive, it's a pattern. Templates, Resolve,
Expansion, Mutate, Validate — these survived because they each do exactly one
thing that nothing else in the system does. Traits survived because they're the
minimal declaration interface between nodes and the orchestrator. Protocols
survived because the lifecycle trigger binding doesn't reduce to a trait or a
kernel without breaking the separation between what's declared and what
executes. Where we found molecules pretending to be atoms — lifecycle presets
are CRUD flags with labels, Select is conditional nodes with a convenience
wrapper — we called them out as sugar, not primitives.

The payoff of a minimal primitive set is that complexity moves to the compiler,
not the runtime. The compiler is where inference, validation, dependency
analysis, and sugar expansion belong — it runs once, it can be opinionated, and
it can evolve independently. The runtime stays dumb: a planner that's a pure
function, an orchestrator that's a loop, a backend that does IO. When someone
wants a new capability — multi-cluster, rollout gates, schema deferral — the
answer should be a new trait, a new kernel, or a new compiler pass. Never a new
orchestrator mode, never a reconciler rewrite, never a special case in the
execution loop. The system grows by composition at the edges, not by mutation at
the core.

## KDL — Kernel Description Language

This document uses a pseudo-language called KDL to describe graphs, nodes,
kernels, and behavior. KDL is not a parsed or compiled language — it is notation
for human communication in design docs and discussions.

### Kernel Composition

Functional notation. Innermost kernel evaluated first:

```
Resolve(Template(manifest))
Expansion(iterator, Resolve(Template(manifest)))
Mutate(transform, Expansion(iterator, Resolve(Template(manifest))))
```

### Node

Struct block. One node = one entry in the graph:

```
Node {
    id:      "deployment"
    kernel:  Resolve(Template(deployment))
    traits:
        lifecycle:      managed
        dependsOn:      ["configmap"]
        readiness:      status.availableReplicas > 0
        propagateWhen:  [exponentiallyReady(pod, pods)]
}
```

### Graph

Contains nodes. Carries execution strategy:

```
Graph {
    strategy: leveled
    maxConcurrency: 5
    nodes:
        Node { id: "configmap", kernel: Resolve(Template(configmap)) }
        Node { id: "deployment", kernel: Resolve(Template(deployment)), traits: ... }
}
```

### Conventions

- KDL blocks use `{}` for structs and indentation for readability
- Traits are key-value pairs under `traits:`
- CEL expressions appear as string values: `readiness: status.ready`
- Lists use `[]`: `dependsOn: ["a", "b"]`
- Comments use `//`
- When Go types are shown alongside KDL, the KDL is the conceptual model, the Go
  is the implementation

## Terminology

- **Graph** — the container. Holds nodes + a planner. Carries execution
  strategy. What the orchestrator executes.

- **Node** — the unit of work; one entry in the graph. Kernel + ID + traits +
  protocols. Holds desired state (kernel output) and observed state (set by
  orchestrator).

- **Kernel** — _what_ the node computes. A pure data transformation. Kernels
  compose by wrapping: each kernel takes an inner kernel and adds one concern.
  The result is a tree inside a node (e.g.
  `Mutate(Resolve(Template(manifest)))`). Primitives: Template, Resolve,
  Expansion(Collections), Mutate, Validate, Select.

- **Trait** — _how_ the orchestrator should treat the node. Traits don't change
  what a node computes; they control scheduling, gating, and routing around it.
  Two kinds:
  - Meta (compile-time data): DependsOn, Lifecycle, Scope
  - Predicates (runtime CEL): IncludeWhen, Readiness, PropagateWhen, FailsWhen

- **Protocol** — _what happens_ on a lifecycle event (finalization, failure). A
  protocol is itself a graph — same nodes, same kernels, same traits, own
  planner. Attached to a node, triggered by an event. Bounded recursion:
  protocol nodes cannot carry protocols (depth 1, enforced by compiler).

- **Planner** — pure function: given nodes + state, returns next batch of steps.
  Encodes execution strategy (serial, leveled, eager) and failure strategy
  (stop, skip, rollback).
- **Orchestrator** — dumb execution loop. Calls planner, executes steps,
  observes results. One struct, not an interface.

Non-runtime layers:

- **Compiler** — transforms frontends (RGD, future formats) into graphs. Owns
  dependency inference, schema validation, import resolution.
- **Backend** — cluster IO (SSA apply, GET, LIST, DELETE, ApplySet). The
  side-effect boundary.
- **Schema** — the graph's function signature. Input/output contract.

```
Graph    = Node[] + Planner
Node     = Kernel + Traits + Protocol[]
Protocol = LifecycleEvent + Graph
```

## Layers

```

┌─ Compiler ─────────────────────────────────────────────────┐
│ Produces graphs IRs from frontends (RGD, future formats).  │
│ Dependency inference, schema validation, import.           │
└────────────────────────────┬───────────────────────────────┘
                             │ produces
                             ▼
┌─ Graph ────────────────────────────────────────────────────┐
│ ┌─ Node ─────────────────────────────────────────────────┐ │
│ │                                                        │ │
│ │  ┌─ Kernel ─────────────────────────────────────────┐  │ │
│ │  │ Computation tree — shapes data                   │  │ │
│ │  │ (Template, Resolve, Expansion, Mutate, Validate) │  │ │
│ │  └──────────────────────────────────────────────────┘  │ │
│ │                                                        │ │
│ │  ┌─ Traits ─────────────────────────────────────────┐  │ │
│ │  │ Meta + predicates — instruct the orchestrator    │  │ │
│ │  │ (DependsOn, Lifecycle, Condition, Readiness,     │  │ │
│ │  │  Scope, Propagation)                             │  │ │
│ │  └──────────────────────────────────────────────────┘  │ │
│ │                                                        │ │
│ │  ┌─ Protocols ──────────────────────────────────────┐  │ │
│ │  │ Lifecycle event + sub-graph (own planner)        │  │ │
│ │  │ (finalization, onFailure)                        │  │ │
│ │  └──────────────────────────────────────────────────┘  │ │
│ │                                                        │ │
│ └────────────────────────────────────────────────────────┘ │
│ N nodes, connected as a DAG. Carries execution strategy.   │
└────────────────────────────┬───────────────────────────────┘
                             │ executed by
                             ▼
┌─ Planner + Orchestrator ───────────────────────────────────┐
│ Planner: pure function (nodes + state -> steps)            │
│ Orchestrator: dumb loop (execute, observe, repeat)         │
└────────────────────────────┬───────────────────────────────┘
                             │ delegates IO to
                             ▼
┌─ Backend ──────────────────────────────────────────────────┐
│ Interface: SSA, GET, LIST, DELETE, ApplySet                │
│ Side-effect boundary — all external IO goes through here.  │
│                                                            │
│  ┌─  Kubernetes ──────┐  ┌─ Machinery ──┐  ┌─ Future ────┐ │
│  │ Direct cluster IO  │  │ In-process   │  │ Remote,     │ │
│  │ via dynamic client │  │ handler      │  │ others .... │ │
│  └────────────────────┘  └──────────────┘  └─────────────┘ │
└────────────────────────────────────────────────────────────┘

```

Schema is the interface/contract of the whole graph (function signature), not a
runtime concept.

## Schema

The graph's function signature. Defines what goes in (instance spec) and what
comes out (instance status). Not a kernel — a compiler primitive that produces
the CRD.

Three parts:

- **Input schema** — the instance spec shape. Fields, types, defaults,
  validation rules. Written in SimpleSchema syntax, compiled to OpenAPI v3 for
  the CRD. This is what users fill in when they create an instance.
- **Output schema** — the instance status shape. Fields and their CEL
  expressions referencing resource observed state. Compiled into the instance
  node's kernel (see Instance Node). This is what the orchestrator populates.
- **Resource schemas** — the GVK schemas of child resources. Used by the
  compiler for CEL type-checking and by the Validate kernel for deferred
  validation. Fetched from the API server's discovery/OpenAPI endpoint at
  compile time (or runtime for deferred cases).

The compiler uses schema to: generate the CRD, type-check CEL expressions, infer
dependencies from data flow, and produce the instance node's kernel. At runtime,
schema is consumed — the CRD is registered, the instance node carries both input
(spec via Load) and output (status via Project).

## Parity Scope (v1)

This design includes both parity behavior and extension points. For
migration/parity discussions, treat this as the required scope:

- Runtime parity primitives: `core/resource`, `dag/composer`,
  `conditional/include`, `expander/forEach`
- `DependsOn`, lifecycle behavior, and readiness behavior preserve current RGD
  semantics
- Compiler infers dependencies; runtime executes explicit graph edges
- Compile-time schema validation remains default when schema sources are known
- Deferred runtime validation is used for schemas that are only resolvable once
  runtime context (for example target cluster) is known

Everything else in this doc can be modeled now, but is extension surface unless
explicitly marked as parity.

## Additive Adoption Plan (No Rewrite)

This is the immediate adoption track for ideas from `kernels-codex.md`:
additions only, no behavior-breaking rewrite.

Will things change a lot now? No.

- RGD user API stays unchanged.
- Legacy execution stays default.
- New contracts are introduced behind internal seams and flags.

Adopt in phases:

1. Add a runtime seam in `pkg/controller/instance`.

- Introduce an internal runtime interface and keep a `LegacyRuntime`
  implementation as default.
- Do not change reconcile behavior yet; just route existing logic through the
  seam.

2. Add runtime contract types without changing semantics.

- Add `ObservePlan` and `EvalOption` contracts to runtime-facing interfaces.
- Keep current internals as implementation, including `GetDesired` +
  `SetObserved` dataflow.
- Keep `ErrDataPending` as explicit retry signal owned by controller requeue
  policy.

3. Add compiler pipeline structure around current builder.

- Keep existing output, but execute through named pass stages (`normalize`,
  `validate`, `link`, `typecheck`, `assemble`).
- Keep pass outputs deterministic and inspectable.

4. Split validation explicitly into two phases.

- Structural validation first (IDs, shape, basic form).
- Semantic validation after expression extraction/type context is available.

5. Add dual artifact emission from one compile run.

- Continue emitting `graph.Graph` for current runtime.
- Add a second artifact slot for future kernel plan/IR (even if initially
  minimal).
- Keep both artifacts generated from the same typed compile context.

6. Add rollout modes for safe migration.

- Engine mode flag: `legacy`, `shadow`, `kernel`.
- `shadow` runs candidate execution path for diffing/telemetry without changing
  applied behavior.

7. Keep large changes out of this phase.

- No package migration requirement.
- No mandatory orchestrator rewrite.
- No runtime dependency inference from CEL at execution time.

## Kernels

Six primitives describe the compiled shape of the resource graph. Purely
structural — they determine WHAT exists, HOW MANY, WHAT TRANSFORMS, HOW TO
MATERIALIZE, and WHETHER SCHEMAS ARE VALID.

In addition, the runtime can inject ephemeral wrapper kernels on demand for a
specific operation intent (apply, observe, delete planning). Those injected
wrappers are not part of the persisted compiled graph.

### Template (leaf)

Inert data. A K8s manifest with CEL expression holes. No behavior. It just holds
data until another kernel acts on it.

### Expansion (wrapper, pre-processing)

How many copies? Evaluates a CEL iterator to a list. Produces one copy of inner
entry per item.

- Can wrap any Entry (including other Expansions for nested loops)
- Multiplier: N

### Mutate (wrapper, post-processing)

Transform the output. Delegates to inner entry, receives the result, applies a
transformation (rewrite API group, inject labels, modify fields).

- Mutations declare phase explicitly: `preResolve` or `postResolve`
- Ordering is deterministic in compiled output (no implicit runtime reordering)
- Enables composition: wrap clean templates with transforms without modifying
  them
- Subtree-level: one Mutate applies to all templates beneath it
- Key use case: multi-cluster (rewrite API group per cluster)
- Can be either compiled into the node's static kernel tree or injected on
  demand by the runtime for a specific operation

### Resolve / Eval (terminal)

Substitution function. Evaluates CEL expressions in a Template against a scope,
producing a concrete manifest with no holes. The step where inert data becomes
concrete output — like `eval` in Lisp or beta reduction in lambda calculus.

- Also a candidate name: **Eval** — short, universally understood, exactly what
  it does
- Strict mode: all expressions must resolve or error (regular resources)
- Soft mode: resolve what you can, skip pending (instance status)
- Resolution mode could be determined by the node's lifecycle directive

### Validate (leaf)

Deferred schema validation. A leaf kernel (like Template) that checks whether
GVKs are valid against a target cluster's schemas. The compiler injects Validate
nodes when schema validation cannot happen at compile time (e.g., target cluster
determined at runtime by observed state).

- Not written by users — compiler-injected when it detects deferred schemas
- Leaf kernel: doesn't wrap anything, sits in its own node
- Takes a list of GVKs to validate and a schema source
- Backend fetches the target schema; the kernel checks validity
- The node's readiness gates the entire downstream subgraph via DependsOn
- Validation happens on raw templates, BEFORE any expansion/mutation/resolution

```go
// Compiler-injected validation node
Node {
    id:      "validate-target-schemas"
    kernel:  Validate(gvks: [Deployment, Service], schemaSource: targetCluster)
    traits:
        lifecycle:  virtual
        dependsOn:  ["vpc"]        // wait until target cluster is known
        readiness:  result.valid
}

// These nodes won't execute until validation passes
Node {
    id:      "deployment"
    kernel:  Expansion(clusters, Resolve(Template(deployment)))
    traits:
        dependsOn:  ["validate-target-schemas"]
}
```

When schemas are known at compile time (the default), the compiler validates
directly and no Validate kernel is emitted. Same validation logic, different
timing.

### Select (multi-way branching)

The sixth kernel. Evaluates CEL predicates at runtime and picks the first
matching entry from a set of candidates. Same pattern as Expansion — Expansion
determines how many at runtime, Select determines which one. One node, one ID,
one output. Each case can wrap a full kernel tree and produce different GVKs.
Downstream never knows which branch was taken.

```
Select([Case(CEL, Entry), ...], Default(Entry))
```

Different from Condition (a binary gate on a node that decides whether the node
exists at all). Select is multi-way selection within a single node — the node
always exists, only its content varies.

Select is a primitive, not a decomposition of conditional nodes. The decomposed
alternative — N conditional nodes with mutually exclusive Conditions plus a
virtual merger node — requires partial resolution or `has()` guards for the
merger to reference conditionally-skipped outputs. Select sidesteps this: one
node, one ID, no merger, no partial resolution.

### Injected Kernels (on-demand wrappers)

These are runtime-injected wrappers, not user-authored primitives and not
persisted in revisions. They are attached around a compiled node kernel for one
evaluation and then discarded.

Injected kernel types:

- **Projection wrapper** — `WithIdentityOnly`: evaluate only identity paths
  needed for targeting (`metadata.name`, `metadata.namespace`, optionally
  target/cluster identity), skipping non-identity fields.
- **Mutation wrapper** — `WithNormalizedNamespace`: default namespace-scoped
  resources to the instance namespace when `metadata.namespace` is omitted, so
  identity and IO routing are stable.
- **Mutation wrapper** — `WithAPIGroupRewrite`: rewrite `apiVersion`/group at
  runtime when target selection requires deferred API targeting (for example
  multi-cluster or target-specific APIs).

Design rule: compiled kernels define stable desired-state semantics; injected
kernels adapt evaluation to operation intent without changing the compiled graph
contract.

## Template Features

### Dynamic Field Keys

Template expressions today only appear in field values. Keys are static, which
lets the compiler walk the manifest structure, resolve field paths, type-check
expressions, and infer dependencies. Dynamic keys — CEL expressions in key
position like `${region}: "true"` — are a natural extension for open map fields
like labels, annotations, and ConfigMap data, where users frequently need
computed keys. The challenge is that dynamic keys break the compiler's ability
to resolve the full field path at compile time, since the final key segment is
unknown until Resolve evaluates the expression.

### Best-Effort Compile Time, Full Validation at Runtime

Rather than a hard rule about where dynamic keys are allowed, the compiler does
as much as it can and delegates the rest. When it encounters a dynamic key, it
stops path-based schema descent at the parent level. It can still validate that
the key expression produces a string, that value expressions are well-formed,
that dependencies are extracted, and that static siblings at the same level are
fully validated. Everything nested under the dynamic key is treated as a CEL
object — type-checked but not schema-validated against the target resource. The
remaining validation is pushed to runtime through the Validate kernel, the same
mechanism used for deferred schema validation in multi-cluster scenarios where
the target isn't known at compile time. The compiler injects a runtime
validation step that checks the fully-resolved manifest against the resource
schema after Resolve produces concrete output.

### Enum-Constrained Keys

When the compiler can determine that a key expression resolves to a bounded set
of values — from a CEL enum type, a SimpleSchema enum declaration, or a literal
list — it can fan out validation across all possible paths. If
`${schema.spec.target}` has enum `["labels", "annotations"]`, the compiler
validates the nested structure against both `metadata.labels` and
`metadata.annotations`, confirming compatibility across all variants. This is
exhaustive pattern matching: if the type system knows all variants, the compiler
checks all branches. The constraint is that the enum must be fully known at
compile time from the schema definition or a literal, not from upstream observed
state.

### Practical Scope

The enum-key optimization is most valuable for metadata-level concerns where the
possible targets share compatible schemas — labels and annotations being the
clearest example, since both are `map[string]string` siblings where any valid
content under one is valid under the other. For cases where a dynamic key would
target structurally different schemas across enum values, Select is usually the
better primitive — pick a whole template variant rather than dynamically
resolving a field path into divergent structures. Enum-constrained key
validation is an advanced compiler optimization, not a v1 requirement, but it
extends naturally from the best-effort compile-time model without new runtime
primitives.

### Optional Field Omission

Templates sometimes need fields that are entirely absent from the output
manifest rather than set to null or empty. A deployment template might include
`minReadySeconds` only when the user provides it, or a service might include
`loadBalancerIP` only for certain configurations. Today, Resolve operates in
strict mode — every CEL expression must resolve or it's an error. Optional field
omission extends the template syntax to let specific fields declare that they
should be dropped from the resolved output when their expression can't resolve
or evaluates to an absent value. The cleanest syntax is a field-level marker:
`field?: ${expr}` signals "omit this field if the expression produces no value."
This mirrors optional field syntax in TypeScript and Go, reads naturally at the
YAML structure level, and makes the intent visible before you even read the
expression. Resolve stays strict by default — `?` is per-field opt-in.

### Complementary CEL Functions

For cases where the omission decision is more complex than "value exists or
not," a CEL-level function like `${expr.orOmit()}` provides programmatic
control. Where `field?:` handles the simple case of absent values, `orOmit()`
handles computed decisions:
`${schema.spec.replicas < 3 ? schema.spec.minReadySeconds.orOmit() : schema.spec.minReadySeconds}`
or conditional omission based on upstream state. A companion function
`${expr.orDefault(fallback)}` covers the case where you want a fallback value
instead of omission. Together, `field?:` for structural optionality and
`.orOmit()` / `.orDefault()` for expression-level control give users a complete
toolkit without changing Resolve's default strict semantics.

### Merge

A CEL function for combining maps —
`${merge(deployment.metadata.labels, {"app": "myapp", "tier": "web"})}` — covers
the most common composition pattern in templates: inheriting upstream metadata
and layering on additional values. Labels, annotations, env var maps, ConfigMap
data — users constantly need to take an existing map and add or override
entries. Merge is a pure CEL function, not a template syntax extension, so it
composes naturally with other expressions and requires no new kernel or Resolve
behavior. Patch — deep structural overlay of nested objects — is a significantly
more complex operation with ambiguous semantics around arrays, null handling,
and merge keys. It's tracked as a future possibility but not in scope. Merge on
flat maps covers the immediate need.

### Escape Characters

Templates sometimes contain literal strings that look like CEL expressions but
aren't — shell scripts in ConfigMap data, Go template syntax meant for
downstream tools, or documentation strings containing `${...}` patterns. Today
Resolve would attempt to evaluate these and error. An escape sequence lets
template authors mark expressions as literal passthrough: `\${USER}` in the
template produces `${USER}` in the resolved output, with Resolve skipping
evaluation entirely. The compiler recognizes the escape at parse time and
preserves the literal content through the Resolve phase. This is the same
pattern as escape characters in every string interpolation system — necessary
once your expression delimiters can collide with the content being templated.

### Soft Dependencies and Deferred Resolution

`${defer(expr)}` introduces soft edges in the dependency graph — dependencies
that the compiler acknowledges but the planner doesn't block on. When the
compiler encounters `defer`, it marks the edge as soft, finds a valid
topological ordering by breaking cycles at soft edges, and annotates the graph
with a `hasDeferredEdges` flag. At runtime, the planner schedules nodes ignoring
soft dependencies. When Resolve hits a `defer()` call whose dependency has no
observed state yet, it omits the field entirely. The node is applied without it,
and execution proceeds. This enables patterns like mutual references between
resources where both need each other's data but neither strictly requires it to
be created — the cycle is broken temporally across passes rather than rejected
at compile time.

The compiler injects a virtual node — like it already does with Validate — that
depends on all nodes in the graph and has a readiness condition: "all defer
expressions are resolved." It's the convergence gate as a first-class node in
the DAG, not a special orchestrator behavior. The orchestrator doesn't need to
know about `hasDeferredEdges` or final passes. It just executes the graph. The
injected node sits at the end, its readiness check fails if any deferred fields
are still unresolved, which triggers a requeue, which re-evaluates the deferred
nodes, which fills in the fields, which satisfies the gate. No special case in
the orchestrator. No final pass logic. Just a compiler-injected node using
existing primitives — virtual lifecycle, readiness predicate, DependsOn. The
compiler already does this for Validate. Same pattern.

## Composition

Kernels compose by wrapping — each wrapper takes exactly one inner entry (1:1
containment), with Select as the exception (N candidates → 1 output). This is
function composition, not aggregation. The outermost kernel controls execution;
evaluation recurses inward.

```
Entry := Expansion(Iterator, Entry)
       | Mutate(Transform, Entry)
       | Select([Case(CEL, Entry)], Default(Entry))
       | Resolve(Template)
       | Template(Manifest)
```

- Expansion and Mutate wrap exactly one Entry (1:1)
- Select takes N candidate entries, produces one output (N:1 → 1 output)
- Template is the only leaf (inert data)
- Resolve is the only terminal (always wraps a Template)
- Nesting depth is arbitrary: Expansion can wrap Mutate can wrap Expansion can
  wrap Resolve(Template)
- Injected wrappers (`WithIdentityOnly`, `WithNormalizedNamespace`,
  `WithAPIGroupRewrite`) are runtime-only and intentionally excluded from the
  compiled Entry grammar

## Examples

```go
// Bare resource
Resolve(Template(deployment))

// Collection
Expansion(schema.spec.zones,
    Resolve(Template(worker)))

// Multi-cluster with mutation (one node per resource)
// Node "deployment":
Expansion(schema.spec.clusters,
    Mutate(apiGroup: clusterRewrite(cluster),
        Resolve(Template(deployment))))
// Node "service":
Expansion(schema.spec.clusters,
    Mutate(apiGroup: clusterRewrite(cluster),
        Resolve(Template(service))))

// Nested expansion
Expansion(schema.spec.clusters,
    Expansion(cluster.zones,
        Resolve(Template(zoneDeployment))))

// Multi-way template selection
Select([
    Case(schema.spec.engine == "postgres", Resolve(Template(pgInstance))),
    Case(schema.spec.engine == "aurora", Resolve(Template(auroraCluster))),
], Default(Resolve(Template(pgInstance))))

```

## Nodes

A node is two things at once:

1. **The unit of work** — a concrete entry in the graph. What the orchestrator
   schedules, executes, and tracks. Each node has an ID, produces output, and
   participates in the DAG.
2. **The boundary** — the interface between kernels (which shape data) and the
   orchestrator (which coordinates execution). The orchestrator never looks
   inside the kernel tree; it only sees the node's output and traits.

A node = kernel tree + ID + traits + protocols.

The compiler produces nodes from RGD resource entries. One RGD resource entry
typically becomes one node, though the compiler may inject additional nodes
(validation, status patching) that don't correspond to user-written entries.

### Traits and Protocols

Traits are declarations attached to nodes. They tell the orchestrator how to
treat a node without changing what the node is. A trait is either a piece of
static data known at compile time (meta traits) or a CEL expression evaluated at
runtime (predicate traits). In both cases, a trait is a single value — never a
workflow, never a sub-graph, never something that triggers execution. The
orchestrator reads traits to make decisions: should I include this node? Is it
ready? Did it fail? What should I wait for? Traits inform the orchestrator's
behavior but don't introduce new execution.

Meta traits are data: DependsOn declares ordering, Lifecycle declares CRUD
policy, Scope declares where the resource lives. They're resolved at compile
time and don't change during execution. Predicate traits are CEL expressions
evaluated against runtime state: Condition decides inclusion, Readiness signals
completion, Propagation gates mutation, Failure detects problems. Predicates are
evaluated by the orchestrator at specific points in the node lifecycle —
condition before eval, propagation before apply, failure and readiness after
observe. Every trait produces a single answer: a boolean, a list of node IDs, a
lifecycle mode. No side effects.

Protocols are lifecycle event + graph pairs. A protocol is a Graph — same nodes,
same kernels, same traits, own planner — triggered by a lifecycle event on the
owning node. Currently: failure (onFailure) and deletion (onDelete). Unlike
traits, which inform decisions, protocols trigger execution. The orchestrator
runs the protocol's graph when the event fires, using the protocol's own planner
for execution strategy.

Protocols exist because some lifecycle events break the normal forward-flowing
DAG model. During deletion, the graph needs to walk in reverse (rollin) — a
regular node can't express "create this snapshot before my parent is deleted."
During failure, the dependency system blocks downstream nodes — a regular node
can't express "activate when my dependency fails." These are counterflows:
workflows that run against the grain of normal execution, triggered by events
where the standard dependency model doesn't apply. Each protocol carries its own
planner because the execution strategy differs — deletion protocols use a rollin
planner (reverse order, teardown), failure protocols might use a serial planner
(strict sequence, no parallelism).

Protocols have strict scoping rules. Because they run in degraded contexts — the
graph may be partially torn down or a node has failed — referencing arbitrary
main-graph nodes is unsafe. Protocol nodes can reference other nodes within the
same protocol, the parent node that triggered the protocol, and the parent's
direct dependencies. The compiler enforces this boundary. Protocol nodes cannot
themselves carry protocols — depth is limited to 1. If a protocol grows complex
enough to need its own failure handling, rollout control, or nested protocols,
any node within it can be an RGD instance — complexity escapes through
composition into a separate controller, not through deeper nesting.

**The distinction:** Traits are what the orchestrator reads. Protocols are what
the orchestrator runs. A trait is a value — static or computed — that shapes how
the orchestrator handles a node during normal execution. A protocol is a graph
that the orchestrator executes when normal execution breaks down. Traits live in
the forward-flowing DAG. Protocols activate when the forward flow is interrupted
or reversed. Traits are always evaluated. Protocols are conditional — they may
never fire if the node never fails and the instance is never deleted.

### Kernel Tree

Inside a node, kernels compose by 1:1 wrapping. The outermost kernel controls
execution; evaluation recurses inward to produce the node's output. The
orchestrator is opaque to this — it calls the node, gets output back. How the
output was produced (simple template, expanded collection, mutated result) is
the node's internal concern.

```
Orchestrator sees:       Node("deployment") → output
Node contains:           Expansion(zones, Mutate(..., Resolve(Template(deploy))))
```

The tree is built once at compile time and reused across reconciles. It is
static structure, not runtime state. On-demand injected wrappers may be layered
around it per operation intent, but they do not mutate the compiled tree.

### Node State

Each node holds two kinds of state:

- **Desired state** — the output of kernel evaluation. What the node wants to
  exist. Produced by the kernel tree (pure compute, no IO).
- **Observed state** — the actual state of the resource as seen by the cluster.
  Set exclusively by the orchestrator after backend performs IO. Kernels never
  set their own observed state.

The data flow loop:

```
1. Kernel evaluates       → desired state (pure compute, no IO)
2. Orchestrator applies   → desired state sent to cluster via backend
3. Orchestrator observes  → actual state read back from cluster
4. Observed state stored  → on the node, by the orchestrator
5. Downstream reads       → upstream observed state via CEL references
```

Downstream kernels only see observed state from upstream nodes — never desired
state. This is what makes kernels pure: they receive facts (what exists), not
intentions (what was requested).

What "observed" means depends on lifecycle:

| Lifecycle | Observed state                                            |
| --------- | --------------------------------------------------------- |
| managed   | what the API server returns after apply                   |
| read-only | what GET returns (no apply)                               |
| virtual   | resolved output itself (no IO — kernel output = observed) |

### Defaults

When traits are absent, a node has sensible defaults:

| Trait                         | Default                                        |
| ----------------------------- | ---------------------------------------------- |
| Lifecycle                     | orphan (create, update, no delete)             |
| Condition                     | always included (true)                         |
| Readiness                     | ready after successful apply (no gate)         |
| Failure                       | only IO errors (no semantic failure detection) |
| DependsOn                     | inferred from CEL data flow                    |
| Propagation (`propagateWhen`) | `[]` — always proceed (no gate)                |
| Protocols                     | none (no lifecycle protocols)                  |

Lifecycle default is not RGD schema behavior. It is injected by
controller/runtime wiring in Go (flag/config) when lifecycle is omitted. Current
shipped fallback is `orphan`.

A bare node with zero explicit traits and no protocols: "apply this resource,
include it always, it's ready once applied, ordering comes from data flow, no
edge gates, orphan on teardown." Traits are overrides.

```go
// Bare node — all defaults
Node {
    id:      "configmap"
    kernel:  Resolve(Template(configmap))
}

// Node with trait overrides
Node {
    id:      "deployment"
    kernel:  Expansion(zones, Mutate(..., Resolve(Template(deploy))))
    traits:
        lifecycle:   managed
        dependsOn:   ["configmap", "database"]
        condition:   schema.spec.ha.enabled
        invariants:  [old == null || old.spec.version <= new.spec.version, old == null || old.metadata.namespace == new.metadata.namespace]
        readiness:   status.availableReplicas > 0
}
```

### Special Node Roles

Some nodes have specific roles determined by their lifecycle and origin. These
aren't separate node types — they're regular nodes with particular trait
configurations.

**Instance node** — the source and sink of the DAG. `lifecycle: instance`. The
planner emits two steps: `Load` at the start (pre-loads CR spec as observed
state, making it available in scope) and `Project` at the end (evaluates the
node's kernel to produce status, patches via `backend.PatchStatus()`). Every
graph has exactly one. All nodes that reference `schema.*` depend on this node
(compiler-inferred). The instance node's traits carry instance-level readiness,
failure, and propagation gates. See Instance Node.

**Resource nodes** — the common case. `lifecycle: managed` when explicitly set.
When omitted, lifecycle resolves to controller-configured default (currently
`orphan`). The orchestrator applies desired state, observes actual state, and
feeds observed state forward.

```go
// Instance node — source + sink (Load at start, Project at end)
Node {
    id:        "source-sink"
    kernel:    Resolve(Template(statusProjection))
    traits:    { lifecycle: instance, readiness: ..., failsWhen: ... }
}

// Resource node — managed
Node {
    id:        "deployment"
    kernel:    Resolve(Template(deployment))
    traits:    { lifecycle: managed, dependsOn: ["source-sink"] }
}
```

### Virtual Nodes

Nodes with `lifecycle: virtual` never touch the cluster — resolved output is the
observed state. No new primitive needed; virtual is just a lifecycle policy. Two
common patterns:

**Variable nodes** — user-declared, produce intermediate computed data that
downstream nodes consume via CEL references. Same kernels (Template, Resolve,
Expansion, Mutate, Condition) apply. A Template without a GVK is just structured
data with CEL holes.

**Validation nodes** — compiler-injected, gate downstream subgraphs via
readiness + DependsOn. Not referenceable as data (compiler constraint, not
runtime constraint). Output is a pass/fail signal, not structured data.

Both share the same lifecycle and runtime behavior. The orchestrator treats them
identically: evaluate kernel, store output, check readiness, move on. The
differences are origin (user vs compiler) and consumption pattern (data flow vs
ordering gate).

Note: the instance node (`lifecycle: instance`) is not virtual — it has its own
lifecycle with `Load` and `Project` steps. See Instance Node.

```go
// Variable node — user-declared, consumed via CEL data flow
Node {
    id:      "zone-configs"
    kernel:  Expansion(schema.spec.zones,
                Resolve(Template({zone: ${zone.name}, cidr: ${zone.cidr}})))
    traits:
        lifecycle:   virtual
        condition:   schema.spec.multiZone.enabled
        readiness:   size(result) > 0
}

// Validation node — compiler-injected, consumed via DependsOn gate
Node {
    id:      "validate-target-schemas"
    kernel:  Validate(gvks: [Deployment, Service], schemaSource: targetCluster)
    traits:
        lifecycle:  virtual
        dependsOn:  ["vpc"]
        readiness:  result.valid
}
```

## Traits (Node Instructions)

Traits are instructions attached to nodes. Declared on nodes, consumed by the
orchestrator. They don't change WHAT a node is — they tell the orchestrator HOW
to treat it.

Two kinds:

### Meta (data, known at compile time)

#### DependsOn

Explicit ordering: this node must wait for the listed nodes before the
orchestrator processes it. Supplements compiler-inferred dependencies from CEL
analysis.

- Inferred dependencies come from CEL references (data flow)
- Explicit dependencies come from user declarations
- The compiler merges both into the DAG; the orchestrator doesn't care about the
  source
- Compile/runtime split: compiler materializes explicit edges; runtime executes
  the explicit DAG and does not infer dependencies from CEL

#### Lifecycle

How should backend interact with this node's resolved manifests? Users write
named presets in the RGD. The compiler decomposes each preset into three boolean
flags (create, update, delete) on the compiled node. At runtime, the
orchestrator and backend only see the flags — no preset names, no enum matching.
Presets are RGD-facing; flags are runtime.

- orphan (default): create, update, skip delete on teardown
- managed: create, update, delete
- read-only: GET only, never apply (replaces externalRef)
- create-only: create but never update
- virtual: no IO at all — resolved output is the observed state (in-memory only)
- reference-counting: future, only delete when no references remain

The default preset is runtime wiring (Go flag/config), not RGD schema.
Compiler/runtime receive this as configuration; current default is `orphan`.

#### Scope

Where does this node's resource live? Tells backend where to route IO.

- namespace (default): resource lives in the instance's namespace
- cluster: resource is cluster-scoped
- explicit namespace: resource lives in a specific namespace (CEL expression)
- target cluster: resource lives on a different cluster (multi-cluster routing)

Namespace is often already in the template's `metadata.namespace`, but cluster
targeting and cross-namespace routing aren't expressible in templates. Scope is
the meta trait that captures routing information the template can't hold.

### Predicates (CEL expressions, evaluated at runtime)

#### Condition

Should this node exist? Evaluates a CEL predicate. If false, the orchestrator
skips the node entirely.

- Contagious: if a node is skipped, all dependent nodes are also skipped
- Parity-v1 restriction: references `schema.*` only (cross-node conditions are
  future)
- Future: partial conditions, graduated inclusion

#### Readiness

Am I done? Evaluates a predicate against this node's observed state (from
backend). When satisfied, signals outward to the orchestrator that dependents
can proceed.

- Direction: outward — "I signal to others that I'm done"
- Evaluates against: this node's observed state
- Parity-v1 restriction: readiness expressions are resource-local (self
  references)
- Can exist at any level (per-resource, per-collection, per-group)
- Per-resource: "is this pod running?"
- Per-collection: "are at least 3 replicas ready?"

#### Propagation (`propagateWhen`)

Should this node's mutation be allowed to proceed? A pre-apply gate that
controls **when** change is permitted. Complementary to `readyWhen` —
propagation gates when mutation starts, readiness gates when it ends. (See
KREP-006.)

`propagateWhen` is a list of CEL expressions. All must be true for the node to
be mutated. If any is false, the node is held — no apply, no update. The
orchestrator rechecks next reconcile.

- Direction: outward — "I gate whether I can be changed"
- Evaluates against: rollout/operational constraints, lifecycle state of other
  resources
- Rate controls: `exponentiallyReady(pod, pods)`,
  `linearlyReady(stage, stages, 1)`
- Time windows: `${maintenance.allowed}`
- Reactive controls: `${!releaseBlockers.data[stage.value]}`
- Custom: `pods.filter(p, p.ready()).size() >= pods.size() * 0.8`

Lifecycle methods available in CEL for all resources:

- `resource.ready()` — true if `readyWhen` conditions are satisfied
- `resource.updated()` — true if updated to the current graph
  generation/revision

Built-in sugar functions:

- `linearlyReady(item, collection, batchSize)` — item can proceed when its batch
  is reached
- `exponentiallyReady(item, collection)` — exponential batching (1, 2, 4, 8...)

`propagateWhen` is the **reusable primitive** across all rollout scenarios:

- Collection rollout = `propagateWhen` on the forEach resource
- Instance rollout = `propagateWhen` on the RGD spec
- Same predicate, same lifecycle methods, same built-ins — different target sets

#### Invariants (`invariants`)

Is this transition legal? Evaluates predicates against transition context before
apply. Invariants are pre-apply validity checks, not rollout gates.

`invariants` is a list of CEL expressions. All must be true for mutation to
proceed. If any is false, the node is marked failed for this reconcile and no
apply happens for that node.

- Direction: inward guard — "is this desired change acceptable?"
- Evaluates against: transition context (`old`, `new`, `schema`/instance input)
- Timing: after desired is resolved and current is observed, before apply
- First create behavior: `old == null` (rules can explicitly allow create)
- Drift control: catches illegal state transitions even when desired compiles
  successfully

Real-world examples:

- Monotonic versioning: `old == null || old.spec.version <= new.spec.version`
- Namespace freeze:
  `old == null || old.metadata.namespace == new.metadata.namespace`
- Name freeze: `old == null || old.metadata.name == new.metadata.name`
- Production downgrade guard:
  `old == null || !(old.spec.tier == "production" && new.spec.tier != "production")`
- CRD compatibility:
  `old == null || kro.isBackwardCompatible(old.spec, new.spec) || schema.metadata.annotations["kro.run/allow-breaking-changes"] == "true"`
- Ownership adoption guard:
  `old == null || old.metadata.labels["kro.run/instance-id"] == new.metadata.labels["kro.run/instance-id"]`

#### Failure

Did I fail? Evaluates a predicate against this node's observed state. When
satisfied, signals failure to the planner, which decides what to do based on the
configured failure strategy.

- Direction: outward — "I signal to the planner that I failed"
- Evaluates against: this node's observed state
- Examples: `status.failed > 0` (Job), `status.phase == "Failed"` (Pod)
- Without failsWhen: failure is only detected by apply errors (IO-level)
- With failsWhen: failure is detected from observed state (semantic-level)

#### Readiness vs Failure vs Invariants vs Propagation vs Condition

Five predicates, evaluated in order by the orchestrator:

```
1. Condition      → should this node exist?          (pre-eval, skip vs include)
2. Propagation    → should this node be mutated now? (pre-apply, propagateWhen gate)
3. Invariants     → is this transition legal?        (pre-apply, old/new validation)
4. Failure        → did this node fail?              (post-observe, first check)
5. Readiness      → is this node done?               (post-observe, only if not failed)
```

Propagation is checked before apply — if `propagateWhen` is false, the node is
held, no mutation happens. This is the key difference from the old model where
propagation was a post-readiness release gate. Now it gates when mutation
_starts_, not when results are _released_.

Invariants are checked after propagation and before apply. If invariants fail,
the node is failed for this reconcile and the planner handles it through normal
failure strategy.

Failure short-circuits readiness. After observe, the orchestrator checks failure
first. A node that matches `failsWhen` is failed regardless of readiness —
there's no reason to evaluate readiness on a failed node. A node that is not
failed is then checked for readiness.

Minimal node state model (v1), evaluated with strict precedence:

1. `Skipped` — Condition is false (or contagious skip from a skipped dependency)
2. `Held` — included, but `propagateWhen` is false (no mutation this cycle)
3. `Failed` — invariant violation, apply error, or `failsWhen` evaluates true
4. `Ready` — not failed, and `readyWhen` evaluates true (or default readiness
   satisfied)
5. `InProgress` — none of the above

Invariant: `Failed` overrides `Ready`.

- Condition = skip (permanent for this reconcile, contagious to dependents)
- Propagation = hold (temporary, pre-apply gate via `propagateWhen`; recheck
  next reconcile)
- Invariants = transition guard (pre-apply legality check via `old/new`; fail on
  violation)
- Failure = signal (outward, triggers planner failure strategy; short-circuits
  readiness)
- Readiness = signal (outward, enables dependent nodes to proceed)

## Protocols

A protocol is a lifecycle event + a graph with its own execution strategy.
Protocols are graphs — same nodes, same kernels, same traits, own planner. They
attach to nodes and activate in response to lifecycle events (`onFailure`,
`finalize`) where the normal forward-flowing DAG model doesn't apply.

The recursive structure: nodes carry protocols, protocols are graphs, graphs
carry nodes. Bounded recursion — protocol nodes cannot themselves carry
protocols (depth 1, enforced by compiler). If a protocol's complexity outgrows
the inline graph, any node within it can be an RGD instance, pushing complexity
into a separate controller.

Each protocol carries its own planner because the execution strategy differs
from the main graph. A deletion protocol needs a rollin planner (reverse order,
teardown). A failure protocol might use a serial planner (strict sequence). The
main graph might use leveled or eager. The orchestrator doesn't care — it runs
graphs. The planner decides how.

Protocols have strict scoping rules because they run in degraded contexts.
During failure or deletion, the broader graph state may be partially torn down
or unreliable. Protocol nodes can reference: other nodes within the same
protocol graph, the parent node (the one that triggered the protocol), and the
parent's direct dependencies. Reaching further up the main graph is discouraged
and should be a compiler warning or error. The compiler enforces these scoping
rules at compile time.

```
Node {
    id:      "database"
    kernel:  Resolve(Template(dbInstance))
    traits:
        lifecycle:   managed
        readiness:   status.ready
        failsWhen:   status.phase == "Failed"

    onFailure: Graph {
        strategy: serial
        nodes:
            Node { id: "snapshot", kernel: Resolve(Template(dbSnapshot)),
                   traits: { lifecycle: orphan, readiness: status.available } }
            Node { id: "incident", kernel: Resolve(Template(pagerdutyIncident)),
                   traits: { dependsOn: ["snapshot"], lifecycle: orphan } }
    }

    onDelete: Graph {
        strategy: serial
        nodes:
            Node { id: "final-snapshot", kernel: Resolve(Template(dbSnapshot)),
                   traits: { readiness: status.available } }
            Node { id: "verify", kernel: Resolve(Template(verifyJob)),
                   traits: { dependsOn: ["final-snapshot"], readiness: status.succeeded > 0 } }
    }
}
```

### Key Rules

- Protocol = lifecycle event + graph + planner. Not a separate runtime primitive
  — it's a graph with a trigger.
- Protocols activate on lifecycle events (`onFailure`, `onDelete`), not during
  normal forward execution
- Protocol graphs are real Graphs — same node/kernel/trait primitives, own
  dependency ordering, own planner
- Each protocol carries its own execution strategy: deletion protocols use
  rollin (reverse order), failure protocols use serial (strict sequence)
- Scope is constrained: protocol nodes can reference within the protocol, the
  parent node, and the parent's direct dependencies — nothing further
- Depth is 1: protocol nodes cannot carry their own protocols
  (compiler-enforced)
- Complexity escapes through composition: make a protocol node an RGD instance
  if you need nested protocols, rollout control, or failure handling within the
  protocol
- No nested failure handling: if a protocol node fails, the protocol is stuck —
  the instance stays in its current lifecycle state (FAILED or DELETING) and
  retries next reconcile

### Failure Protocol Sequence

1. `failsWhen` triggers → node is Failed
2. Orchestrator runs `onFailure` protocol (create resources, wait for readiness)
3. If protocol completes → planner executes graph-level failure strategy
4. If protocol node fails → stuck, instance stays FAILED, retry next reconcile

Without `onFailure`: failure is detected, strategy executes immediately. With
`onFailure`: failure is detected, protocol runs first, then strategy executes.

### Deletion Protocol Sequence

1. Kubernetes sees finalizer on instance, holds deletion
2. Orchestrator walks DAG, finds node with `onDelete` protocol
3. Runs `onDelete` protocol (create resources, wait for readiness)
4. If protocol completes → deletes the parent node's resources
5. If protocol node fails → stuck, instance stays DELETING, retry next reconcile
6. When all nodes are cleaned up → orchestrator removes finalizer → Kubernetes
   deletes instance

### Finalizer Placement

One Kubernetes finalizer on the **instance CR**, never on children. The
finalizer blocks instance deletion until all `onDelete` protocol graphs complete
and all resources are cleaned up. Children never get kro finalizers — underlying
controllers can delete or recreate child resources freely during normal
operation. kro's concern is the instance's deletion lifecycle, not the
children's.

## Recursive Graph Execution

The compiler produces rootless, immutable compiled graphs — unordered lists of
nodes with dependency edges and a strategy hint. A compiled graph is a function
definition, not a function call. At runtime, the orchestrator binds a root (the
instance CR) and a scope (a flat map of all observed state accumulated so far),
then hands the graph to a planner. The planner receives one graph at a time,
imposes its own execution order from the dependency edges, and returns steps. It
knows nothing about scope, parents, or protocols.

Nodes can carry protocols keyed by lifecycle event (`onDelete`, `onFailure`).
When the orchestrator detects a lifecycle event on a node, it calls the same
`execute(graph, root, scope)` function recursively — the protocol's root becomes
that node, the scope carries forward unchanged, and the planner plans the
protocol graph like any other graph. Deletion is not a special mode — the
roll-in planner walks the same main graph in reverse topological order, entering
each node's `onDelete` protocol before deleting that node. One mechanism,
recursive, all the way down.

```
// Compiler output — rootless, immutable
Graph {
    strategy: leveled
    nodes:

        Node {
            id:      "config"
            kernel:  Resolve(Template(configmap))
            traits:
                lifecycle: managed
        }

        Node {
            id:      "database"
            kernel:  Resolve(Template(dbInstance))
            traits:
                lifecycle:  managed
                dependsOn:  ["config"]
                readiness:  status.ready
                failsWhen:  status.phase == "Failed"

            onDelete: Graph {
                strategy: serial
                nodes:
                    Node {
                        id:      "snapshot"
                        kernel:  Resolve(Template(dbSnapshot))
                        traits:
                            lifecycle: orphan
                            readiness: status.available
                    }
            }

            onFailure: Graph {
                strategy: serial
                nodes:
                    Node {
                        id:      "incident"
                        kernel:  Resolve(Template(pagerdutyIncident))
                        traits:
                            lifecycle: orphan
                    }
            }
        }

        Node {
            id:      "deployment"
            kernel:  Resolve(Template(deployment))
            traits:
                lifecycle:  managed
                dependsOn:  ["database"]
                readiness:  status.availableReplicas > 0
        }
}

// Runtime binds root and executes:
// execute(graph, root=instanceCR)
//   → planner orders: config → database → deployment
//   → runtime state accumulates observed state via SetObserved(...)
//   → if database fails: execute(database.onFailure, root=database)
//   → if instance deleted: roll-in planner reverses the same graph
//       → at database: execute(database.onDelete, root=database)
```

```go
type CompiledGraph struct {
    Nodes    []Node
    Strategy StrategyConfig
}

type Node struct {
    ID       string
    Kernel   Kernel
    Traits   Traits
    Graphs   map[LifecycleEvent]*CompiledGraph
}

type Orchestrator struct {
    planner  Planner
    runtime Runtime
}

// One recursive function.
func execute(graph *CompiledGraph, root *Node) {
    state := NewRuntimeView(graph, root)
    for {
        steps, done := planner.Next(state)
        if done { break }

        for _, step := range steps {
            node := state.Node(step.NodeID)
            runtime.RunStep(ctx, step, node, backend)
            state.Update(node)
            if event := detectEvent(node, state); event != nil {
                execute(node.Graphs[event], node)
            }
        }
    }
}
```

## Orchestrator

One orchestrator struct. Strategy variation comes from the planner function, not
from different orchestrator types.

### Planner / Orchestrator Split

The **planner** is a pure function: given the graph and current state, return
the next batch of steps to execute. The **orchestrator** is a dumb loop: execute
steps, observe results, call planner again.

```go
type Step struct {
    NodeID string
    Op     Op    // Load | Reconcile | Project | Delete
}

type Op string

const (
    OpLoad      Op = "Load"
    OpReconcile Op = "Reconcile"
    OpProject   Op = "Project"
    OpDelete    Op = "Delete"
)

type Planner interface {
    Next(view RuntimeView) ([]Step, bool)
}

type Runtime interface {
    RunStep(ctx context.Context, step Step, node Node, m Backend) error
}

type Orchestrator struct {
    planner   Planner
    runtime  Runtime
    backend Backend
}
```

The orchestrator loop:

```go
for {
    steps, done := o.planner.Next(view)
    if done { break }

    for _, s := range steps {
        node := view.Node(s.NodeID)
        if err := o.runtime.RunStep(ctx, s, node, o.backend); err != nil {
            if runtime.IsDataPending(err) {
                return requeue
            }
            return err
        }
        view.Update(node)
    }
}
```

### Status Patching

Instance status is produced by the instance node's kernel — evaluated via a
`Project` step at the end of each planner cycle. The orchestrator evaluates the
kernel against full scope and calls `backend.PatchStatus()` with the result. One
eval, one patch per cycle. See Instance Node.

### Execution Strategies

Three planners over the same DAG. A spectrum of aggressiveness. All respect the
same trait gates (readiness, failure, propagation, condition).

| Strategy | Batch selection                             | Failure boundary          | maxConcurrency            |
| -------- | ------------------------------------------- | ------------------------- | ------------------------- |
| Serial   | one eligible node at a time                 | the failed node           | always 1                  |
| Leveled  | all eligible nodes at same DAG depth (wave) | the entire wave           | optional cap per wave     |
| Eager    | all nodes whose deps are satisfied          | all in-flight + completed | optional cap on in-flight |

`maxConcurrency` is a planner construction parameter, not a per-call value:

```go
planner := NewLeveledPlanner(Config{
    MaxConcurrency:  5,
    FailureStrategy: FailFast,
})
```

Serial is implicitly `maxConcurrency=1`. Leveled and Eager take an optional cap
(0 = unlimited).

### Failure Strategies

When a node's `IsFailed()` returns true (or apply errors), the planner decides:

| Strategy        | On node failure                                              |
| --------------- | ------------------------------------------------------------ |
| FailFast        | return nil — stop                                            |
| SkipAndContinue | skip failed + dependents, continue independent nodes         |
| Retry           | return same node again (requeue)                             |
| RollbackWave    | return Delete steps for completed nodes in current wave only |
| RollbackAll     | return Delete steps for all completed nodes across all waves |

The node reports failure. The planner decides the response. The orchestrator
executes it. Strategy is configured per-graph, not per-node.

**Future extension: per-wave failure policies.** A graph could declare different
failure strategies per wave (e.g., wave 1 does RollbackWave, wave 3 does
RollbackAll). Deferred from v1 — per-graph is sufficient for parity. When added,
it becomes a wave-level config on the planner, not a new abstraction.

### Protocol Execution

Not a separate orchestrator. Protocols are graphs — the orchestrator calls the
same `execute(graph, root, scope)` function recursively. See Recursive Graph
Execution.

**On failure:** If a node's `failsWhen` triggers and the node has an `onFailure`
protocol, the orchestrator calls `execute(node.onFailure, node, scope)` before
the main graph's failure strategy executes. If the protocol completes, the main
planner proceeds (stop, skip, rollback). If a protocol node fails, the node
stays Failed and retries next reconcile.

**On deletion:** If a node has an `onDelete` protocol, the orchestrator calls
`execute(node.onDelete, node, scope)` before deleting that node's resources. If
the protocol completes, the node's resources are deleted. If a protocol node
fails, the instance stays in DELETING and retries next reconcile.

Protocol execution state lives on the owning node in instance status.

### Deletion

No ownerReferences. kro never sets ownerReferences on child resources. Deletion
is not a special mode — the roll-in planner walks the same main graph in reverse
topological order, entering each node's `onDelete` protocol before deleting that
node. Same `execute(graph, root, scope)` function, different planner. See
Recursive Graph Execution.

When the instance is deleted:

1. Kubernetes holds deletion (finalizer `kro.run/managed` on instance)
2. Roll-in planner walks the same graph in reverse topological order
3. At each node: if node has `onDelete` graph →
   `execute(node.onDelete, node, scope)`, wait for completion
4. Issues explicit `Delete()` calls for that node's resources
5. Requeues. Next reconcile continues reverse walk. Repeat.
6. When all nodes report no objects, orchestrator removes finalizer
7. Kubernetes deletes the instance

Deletion planning uses on-demand injected kernels to compute stable targets
before issuing deletes:

- Evaluate node kernel with `WithIdentityOnly`
- Apply `WithNormalizedNamespace` (and `WithAPIGroupRewrite` when target routing
  requires it)
- Compare desired identities with observed objects to select deletion targets
  (collections use ordered intersection in desired order)

Collections are discovered via LIST with label selector
(`instance-id + node-id`). Individual resources via GET using resolved identity.

Why no ownerReferences:

- Cross-namespace resources can't have ownerReferences (K8s constraint)
- Shared ownership / reference-counting is incompatible with single-owner GC
- Explicit deletion gives the orchestrator control over ordering and protocols
- ApplySet labels provide membership tracking without GC coupling

### Status Patch Frequency

One status patch per reconcile cycle. The orchestrator runs the full planner
loop — evaluate, apply, observe, check predicates — then patches instance status
once with everything that changed. Not per-step, not per-node. One write at the
end of the cycle.

This means the crash window is one reconcile cycle. If the controller crashes
between applying a resource and patching status, status is stale — it doesn't
reflect the apply that happened. This is safe because SSA apply is idempotent.
Next reconcile, the orchestrator re-evaluates the graph, re-applies (no-op via
SSA), observes, and patches status. The resource doesn't get double-created or
corrupted. The only cost is one redundant apply.

For large collections during active rollouts, one status patch per cycle is one
patch per requeue interval. If the requeue interval is 10 seconds and a
collection has 500 items rolling out, that's ~6 status patches per minute. Each
patch includes the full `status.nodes` map. This is comparable to a Deployment
controller patching its status during a rollout — acceptable API server load for
the operational benefit.

The alternative — patching after every individual step — would give tighter
crash recovery (never more than one step behind) but multiply API server writes
by the number of nodes processed per cycle. For a leveled planner processing 10
nodes per wave, that's 10x the writes. Not worth it. The one-cycle crash window
with SSA idempotency is the right tradeoff.

## Instance Node

The instance node is a regular node in the graph's node list. It represents the
instance CR — both its spec (input to the graph) and its status (output of the
graph). Its observed state is the CR's spec, pre-loaded before other nodes
execute. Its kernel is the projection — a `Resolve(Template(...))` that produces
the complete instance status from all nodes' observed state. Its traits carry
instance-level readiness, failure, and propagation gates.

The instance node appears twice in the execution plan. The planner sees
`lifecycle: instance`, recognizes it has no dependencies (for loading) and all
nodes depend on it (for data flow), and emits two steps: a `Load` step at the
start and a `Project` step at the end. `Load` tells the orchestrator to pre-load
the CR's spec as the node's observed state, making it available in scope for all
downstream nodes. `Project` tells the orchestrator to evaluate the node's kernel
against full scope and send the result to `backend.PatchStatus()`. Between these
two steps, every other node in the graph executes normally.

This eliminates the need for a special field on the compiled graph. The instance
node is in the node list like everything else. The planner handles the
scheduling. The orchestrator handles the IO routing — `Load` reads from the CR,
`Project` patches status. The instance node's traits work like any other node's
traits: readiness determines when the instance is ready, failure determines when
it's failed, propagateWhen gates when mutations are allowed. No special
instance-level health checks, no separate readiness logic — just traits on a
node.

### Projection Kernel

The instance node's kernel is the projection — a `Resolve(Template(...))` that
produces the complete instance status. The compiler builds it by walking two
sources.

First, the RGD schema's status definitions — users write plain CEL expressions
like `${database.status.endpoint}`. The compiler wraps each with `orOmit()`
automatically, because status fields are inherently incremental and dependencies
resolve at different times.

Second, the compiler walks the node list and emits a `kro.nodeStatus("nodeID")`
call for every node. `kro.nodeStatus()` is a CEL function the orchestrator
provides at runtime — it returns phase, timestamps, revision, and resource
identity from execution state. For collection nodes the compiler emits
`kro.collectionStatus("nodeID")` which includes per-item state and a summary.
For nodes with protocol sub-graphs the compiler adds
`kro.protocolStatus("nodeID", "onDelete")` entries. The compiler knows the full
graph shape, so it can produce the complete operational status structure at
compile time — the orchestrator just fills in the values.

The compiler merges both into a single `Resolve(Template(...))` kernel on the
instance node. There is no distinction between user-defined and operational
status at runtime. One kernel, one eval, one patch.

```
Graph {
    strategy: leveled
    nodes:

        Node {
            id:      "source-sink"
            kernel:  Resolve(Template({
                         "endpoint":  ${database.status.endpoint.orOmit()},
                         "replicas":  ${deployment.status.availableReplicas.orOmit()},
                         "nodes": {
                             "database":    ${kro.nodeStatus("database").orOmit()},
                             "deployment":  ${kro.nodeStatus("deployment").orOmit()}
                         }
                     }))
            traits:
                lifecycle:     instance
                readiness:     nodes.database.phase == "Ready" && nodes.deployment.phase == "Ready"
                failsWhen:     nodes.filter(n, n.phase == "Failed").size() > 0
                propagateWhen: ${maintenance.allowed}
        }

        Node {
            id:      "database"
            kernel:  Resolve(Template(dbInstance))
            traits:
                lifecycle:  managed
                dependsOn:  ["source-sink"]
                readiness:  status.ready
        }

        Node {
            id:      "deployment"
            kernel:  Resolve(Template(deployment))
            traits:
                lifecycle:  managed
                dependsOn:  ["source-sink", "database"]
                readiness:  status.availableReplicas > 0
        }
}

// Planner produces:
// Step { Node: "source-sink",  Op: Load }       ← pre-load spec into scope
// Step { Node: "database",    Op: Apply }
// Step { Node: "deployment",  Op: Apply }
// Step { Node: "source-sink",  Op: Project }    ← eval projection, patch status
```

```go
type CompiledGraph struct {
    Nodes    []Node
    Strategy StrategyConfig
    // No Instance field. No Projection field.
    // The instance node is just a node in the list.
}
```

**Future:** with the instance node named `source-sink`, the CEL reference prefix
`schema.spec.*` should become `source.spec.*` (or `source-sink.spec.*`) to align
with the node ID. Deferred — `schema.*` is the current convention and changing
it is a breaking API change.

## Instance Status as Operational State

Instance status is the single source of truth for all orchestration state. The
orchestrator reads and writes it every cycle. Per node, it tracks phase,
timestamps (applied, ready, failed), revision, protocol execution state, and the
concrete resource identity — name, namespace, GVK. This mapping from graph
identity (node ID) to cluster identity (name/namespace/GVK) is what allows the
orchestrator to coordinate graph-level decisions with actual cluster IO. For
collection nodes, each item gets its own entry with resource identity and
individual state, plus a summary for quick aggregation. All `kro.*` CEL
functions resolve against instance status — `kro.elapsed()` reads timestamps,
`kro.revision()` reads per-node revision, `kro.protocol()` reads protocol state.
On controller restart, instance status is authoritative. The orchestrator picks
up where it left off without scanning the cluster. The planner gets everything
it needs in a single read — no LIST operations for diff computation, rename
detection works because the same node ID can show a different name between
revisions, and collection rollout progress is precise per item.

## Child Resource Labels and Annotations

Labels and annotations on child resources serve discovery and debugging — never
orchestration decisions. Labels (`kro.dev/instance`, `kro.dev/node-id`,
`kro.dev/revision`) are membership selectors used by backend for LIST queries
during deletion and collection management. They answer "which resources belong
to this instance and node?" Annotations carry informational metadata useful for
humans running `kubectl describe` or external tooling that needs to trace a
resource back to its owning graph. The orchestrator writes both during apply but
never reads them back for decision-making. If instance status is ever lost or
corrupted, labels are the recovery mechanism — the orchestrator can rediscover
owned resources via label selectors and rebuild instance status from the
cluster. This is the fallback path, not the normal path.

```yaml
status:
  revision:
    current: 4
    target: 5
  nodes:
    deployment:
      phase: Ready
      revision: 5
      appliedAt: "2025-01-15T10:00:00Z"
      readyAt: "2025-01-15T10:02:30Z"
      resource:
        apiVersion: apps/v1
        kind: Deployment
        name: myapp-deployment
        namespace: default
    service:
      phase: Ready
      revision: 5
      appliedAt: "2025-01-15T10:02:35Z"
      readyAt: "2025-01-15T10:02:40Z"
      resource:
        apiVersion: v1
        kind: Service
        name: myapp-service
        namespace: default
    workers:
      phase: InProgress
      revision: 5
      summary:
        total: 50
        ready: 30
        updated: 35
      items:
        - name: worker-us-east
          namespace: default
          phase: Ready
          revision: 5
          appliedAt: "2025-01-15T10:00:00Z"
          readyAt: "2025-01-15T10:01:00Z"
        - name: worker-us-west
          namespace: default
          phase: InProgress
          revision: 5
          appliedAt: "2025-01-15T10:03:00Z"
  protocols:
    database:
      onFailure:
        state: completed
        startedAt: "2025-01-15T10:05:00Z"
        completedAt: "2025-01-15T10:06:30Z"
```

## Versioning

### What a Revision Is

A revision is an immutable snapshot of a compiled graph, stored as a Kubernetes
CR (`ResourceGraphRevision`). When a user updates an RGD, the compiler produces
a new graph and stores it as a new revision. Revisions are immutable once
created. Retention count is user-configurable; all in-use revisions are always
retained.

Revisions change the graph, not the schema. The CRD (instance shape) stays the
same across revisions. Schema changes are a separate concern (CRD versioning),
not covered here.

### Declarative Target

An instance declares its target revision. The controller reconciles toward it.
"I want revision 5" — the controller figures out how to get there from wherever
the instance currently is.

Two modes of propagation, both controlled by `propagateWhen` (KREP-006):

- **Across instances:** `propagateWhen` on the RGD spec gates which instances
  can be mutated to the new revision. The RGD is itself implicitly a collection
  — each instance is a member. `exponentiallyReady(application, applications)`
  rolls out to instances progressively.
- **Within an instance:** `propagateWhen` on individual resources gates how the
  graph transition happens. This is just a reconcile against the target
  revision's graph, with per-node propagation gates.

No separate rollout CR or rollout strategy enum. `propagateWhen` is the
mechanism at both levels.

### Graph Diff

When a new revision is created, the compiler computes a graph-level diff against
the previous revision. This diff is by node ID and uses compile-time information
only — no CEL evaluation, no cluster state.

What the diff captures:

| Change              | Detection                                  |
| ------------------- | ------------------------------------------ |
| Node added          | node ID in v2, not in v1                   |
| Node removed        | node ID in v1, not in v2                   |
| Template changed    | pre-resolution template comparison         |
| Traits changed      | DependsOn, readiness, condition, lifecycle |
| Kernel tree changed | kernel composition differs                 |
| Unchanged           | template + traits identical                |

The diff is stored on the revision CR. It enables:

- **Skip unchanged nodes.** If a node's template + traits are identical across
  revisions, don't re-apply. The orchestrator skips it entirely.
- **Plan the rollout.** The planner knows which nodes changed before executing
  anything.
- **Observability.** Users can see what will change: "revision 5 → 6: deployment
  template changed, ingress added, cache removed."
- **Early removal detection.** Removed nodes are known upfront, not discovered
  at prune time.

What the diff does NOT capture (requires runtime evaluation):

- Resolved resource identities (namespace/name with CEL expressions)
- Collection item sets (Expansion output depends on runtime scope)
- Whether a "template changed" node produces a different resource identity

The actual transition is still a reconcile. The orchestrator runs the target
revision's graph in dependency order:

1. Check diff — skip unchanged nodes
2. Evaluate changed/added nodes → desired state
3. Apply via SSA → cluster reconciles from current to desired
4. Membership tracking prunes removed nodes, respecting lifecycle policy

The diff is the plan. The reconcile is the execution.

### Two Identities

**Node ID** = graph identity. Compile-time. Used for DependsOn, CEL references,
planner coordination, graph diff.

**Resource identity** = GVK + namespace/name (+ target cluster for
multi-cluster). Runtime. What the cluster sees. Used for apply, observe, prune,
membership tracking.

Both identities are needed. They serve different purposes and can diverge across
revisions.

Resource identity is only known after kernel evaluation (names can contain CEL
expressions). Node ID is always known at compile time.

**Which is the primary anchor for revision diffing? — UNRESOLVED**

**Approach 1: Resource identity primary.** The revision system diffs by what the
cluster sees. A node rename across revisions is invisible if the resource
identity didn't change — SSA updates in-place, old membership entry gets pruned.
Node IDs are internal wiring. The graph diff (by node ID) is an optimization
layer on top, not the primary identity.

**Approach 2: Node ID primary.** Node ID is the stable anchor that persists
across revisions. The instance status inventory maps nodeID → current cluster
resources (namespace/name/GVK). This enables the planner to detect resource
identity changes: same nodeID, different namespace/name between revisions. The
diff algorithm matches by nodeID first, then matches remaining unmatched entries
by resource identity to detect node renames.

Two-pass diff algorithm (approach 2):

```
1. Match by nodeID:
   - In both revisions → check if resource identity changed
   - Only in v2 → added node
   - Only in v1 → removed node
2. Match remaining unmatched by resource identity (GVK + namespace/name + cluster):
   - Match found → node rename (same resource, different nodeID)
3. Still unmatched:
   - v2 only → true create
   - v1 only → true delete
4. Both nodeID and resource identity changed → no anchor, delete + create
```

Approach 2 is more robust for detecting identity changes and renames. Approach 1
is simpler and works when node IDs are stable (the common case).

### Identity Changes

When a node's resource identity changes between revisions (same nodeID,
different namespace/name — e.g., template renames the resource), the
orchestrator must order operations safely:

1. Create the resource with the new identity
2. Wait for readiness
3. Delete the resource with the old identity

This avoids a downtime gap where downstream dependencies have nothing to
reference. Essentially blue-green at the resource level.

If both nodeID and resource identity change simultaneously, there's no anchor to
detect the relationship. The system treats it as an unrelated delete + create.
This is the correct behavior — there's no basis for assuming they're related.

### Collection Rollout

Collection nodes (Expansion) produce N resources. When a revision changes the
template, rolling out the change to all N items at once may be undesirable.

Collection rollout is controlled by `propagateWhen` on the resource — the same
primitive used for instance rollout (KREP-006). No separate rollout trait or
strategy enum.

```yaml
- id: workers
  forEach:
    - worker: ${schema.spec.zones}
  propagateWhen:
    - exponentiallyReady(worker, workers)
  template:
    apiVersion: v1
    kind: Pod
    metadata:
      name: worker-${worker.name}
    spec: ...
  readyWhen:
    - ${worker.status.phase == "Running"}
```

The compiled node:

```
Node {
    id: "workers"
    kernel: Expansion(schema.spec.zones, Resolve(Template(pod)))
    traits:
        lifecycle: managed
        propagateWhen: [exponentiallyReady(worker, workers)]
        readiness: worker.status.phase == "Running"
}
```

`propagateWhen` gates each item's mutation. `readyWhen` signals each item's
completion. Together they control the rollout: propagation decides when to
start, readiness decides when it's done.

Different rollout strategies are just different `propagateWhen` expressions:

- Serial: `linearlyReady(worker, workers, 1)`
- Batched (5 at a time): `linearlyReady(worker, workers, 5)`
- Exponential: `exponentiallyReady(worker, workers)`
- 80% canary gate: `workers.filter(w, w.ready()).size() >= workers.size() * 0.8`
- Time-gated: `${maintenance.allowed}`
- Composable:
  `[exponentiallyReady(worker, workers), ${!releaseBlockers.data[worker.zone]}]`

Item identity is namespace/name. The orchestrator evaluates the Expansion (after
dependencies are satisfied), gets the new desired set, compares against cluster
state by namespace/name, and applies changes per `propagateWhen`.

| Item diff                             | Action                            |
| ------------------------------------- | --------------------------------- |
| Same namespace/name, template changed | update (gated by `propagateWhen`) |
| Only in new desired set               | create (gated by `propagateWhen`) |
| Only in old membership                | prune (lifecycle policy)          |

**Stable identity for collection items.** The compiler should enforce that
Expansion templates derive names from the iterator value, not array index.
Index-based naming breaks identity when the iterator list reorders. Convention:
`worker-${zone.name}`. The compiler warns if an Expansion template name doesn't
reference the iterator variable.

Name derivation change (e.g., `worker-${zone}` → `${zone}-worker`) = full
replace (all old items pruned, all new items created). No progressive rollout
possible.

**Failure bubbling.** Collection-level failure bubbles up as node-level failure.
The `failsWhen` predicate can reference collection-level aggregates (e.g.,
`workers.filter(w, w.status.phase == "Failed").size() > 3`). Once crossed, the
node is Failed and the graph-level failure strategy takes over. Two clean
layers, not interleaved.

### Tracking — RESOLVED

Instance status is the single source of truth for orchestration state. See
Instance Status as Operational State.

Every managed resource is stamped with labels:

```
kro.dev/instance: myapp-123
kro.dev/node-id: deployment
kro.dev/revision: 5
```

Collection items additionally: `kro.dev/collection-index: 3`.

Labels serve discovery and debugging — never orchestration decisions. See Child
Resource Labels and Annotations. Labels are the recovery fallback: if instance
status is lost or corrupted, the orchestrator rediscovers owned resources via
label selectors and rebuilds status from the cluster.

### Drift Detection

The resource inventory on instance status enables three kinds of drift
detection:

| Drift                                    | Detection                                                       | Response                              |
| ---------------------------------------- | --------------------------------------------------------------- | ------------------------------------- |
| Resource missing from cluster            | inventory says it exists, GET returns 404                       | re-apply to restore                   |
| Resource exists but not owned            | resource in cluster with no kro labels, conflicts with desired  | ownership conflict warning            |
| Name/namespace changed between revisions | same nodeID, different resource identity in inventory vs target | identity change (blue-green handling) |

Without an inventory, renamed resources become orphans that leak forever. Drift
detection is especially important for long-lived instances that span many
revisions.

### Rollback

Rollback = set the target revision to a previous one. The controller reconciles
toward it using the same mechanism — run the old revision's graph, SSA applies
the old desired state, membership tracking prunes anything the old revision
doesn't produce.

Trigger: manual (user sets target revision) or automatic (failure strategy
triggers revert).

Rollback to any retained revision, not just the previous one. "Roll to revision
3" works if revision 3 is retained.

Template-only rollback is clean: same resource identity, SSA reverts desired
state. No resource creation or deletion.

Rollback of added nodes (exist in new revision, not in old): resources get
pruned.

Rollback of removed nodes (existed in old revision, were pruned during
transition): resources get recreated from the old revision's template. Original
observed state (labels added by other controllers, etc.) is lost — the resource
comes back fresh.

### Revision Change Mid-Rollout

What happens when a new revision arrives while a previous rollout is still in
progress?

With `propagateWhen`, this is handled naturally — same as Kubernetes
Deployments. The new revision becomes the desired state. The graph is
re-evaluated from the root. `updated()` now evaluates against the new target
revision. Resources that were `updated()` for the old revision are now
not-updated. `propagateWhen` re-evaluates, and propagation proceeds toward the
new target.

Resources that already completed the old rollout proceed to the new revision.
Resources still in progress skip the intermediate revision and go straight to
the newest target. There's no explicit "Supersede" or "Queue" strategy — the
system reconverges toward the latest desired state, always.

This mirrors Kubernetes Deployment behavior: a mid-rollout spec change pivots
the rollout toward the new desired state. The old rollout is implicitly
superseded.

The `propagateWhen` expressions control the rate. `exponentiallyReady` measures
progress toward the current target — when the target changes, progress resets
and the exponential ramp restarts. `linearlyReady` recalculates batches against
the new total of outdated items.

Overlapping propagations can result in up to O(n) simultaneous revisions across
resources in a graph. The `ResourceGraphRevision` CR tracks a monotonically
increasing `spec.number`. Before reconciling a resource, the controller checks
whether its dependencies have a revision number greater than its own — if so,
the older propagation yields to the newer one.

Instance deletion takes priority — any in-progress rollout is abandoned
immediately and the controller enters the deletion flow (protocols + cleanup). A
rollout is meaningless if the instance is being deleted.

### Open Questions

- **Primary identity anchor — RESOLVED:** nodeID primary. Instance status maps
  nodeID → resource identity (name/namespace/GVK). Rename detection works via
  same nodeID showing different resource identity across revisions. See Instance
  Status as Operational State.
- **Tracking approach — RESOLVED:** Instance status is the single source of
  truth (full resource inventory). Labels are the recovery fallback, not the
  primary. See Instance Status as Operational State and Child Resource Labels
  and Annotations.
- **Instance status size limits:** Graphs with large Expansions produce hundreds
  of resource entries. Collection summaries reduce read cost, but per-item
  entries are needed for precise rollout tracking. If status exceeds etcd object
  size limits (~1.5MB), may need a separate CR or pagination strategy.
- **Status write frequency — RESOLVED:** One status patch per reconcile cycle.
  Crash window is one cycle; SSA idempotency makes this safe. See Status Patch
  Frequency.
- **Operational vs user-defined status — RESOLVED:** The instance node's kernel
  unifies both. The compiler merges user-defined status expressions and
  operational status (`kro.nodeStatus()`, `kro.collectionStatus()`,
  `kro.protocolStatus()`) into a single `Resolve(Template(...))` kernel on the
  instance node. No distinction at runtime. See Instance Node.
- **`propagateWhen` scope:** Does `propagateWhen` apply only to revision
  changes, or also to normal reconciles where the instance spec changes (causing
  Expansion output to change)? KREP-006 applies it to all mutations (create and
  update). This means first deploy is also gated by `propagateWhen` if set —
  which may or may not be desirable.
- **Concurrent propagation limit:** KREP-006 discusses allowing O(n)
  simultaneous propagations. Should there be a cap? Kubernetes Deployments allow
  exactly one concurrent rollout. Allowing more is more powerful but adds
  complexity to `updated()` semantics — updated relative to which revision?
- **`ResourceGraphRevision` CR shape:** What fields? Compiled graph bytes? Hash?
  Parent revision reference? Monotonically increasing `spec.number`?
- **Collection-level × graph-level failure interaction:** If a collection node
  can't complete its item rollout, does the planner halt the entire graph
  revision? The failure bubbling (collection → node → graph) is defined, but the
  threshold semantics need more detail.
- **`updated()` semantics across overlapping propagations:** When multiple
  revisions are in flight, what does `updated()` mean? Updated to latest?
  Updated to the revision currently being rolled out? This affects
  `exponentiallyReady` and `linearlyReady` correctness.

## Open Problems

### Instance source-sink — RESOLVED

The instance CR is both the source (provides spec) and the sink (receives
status). This is resolved by the instance node — a single node with
`lifecycle: instance` that represents both roles.

**Source = Load step.** The planner emits a `Load` step at the start. The
orchestrator pre-loads the CR's spec as the instance node's observed state,
making it available in scope for all downstream nodes. All nodes referencing
`schema.*` depend on the instance node (compiler-inferred).

**Sink = Project step.** The planner emits a `Project` step at the end. The
orchestrator evaluates the instance node's kernel against full scope and patches
the result onto the instance via `backend.PatchStatus()`. The compiler merges
user-defined status expressions and operational status (`kro.nodeStatus()`,
`kro.collectionStatus()`, `kro.protocolStatus()`) into a single
`Resolve(Template(...))` kernel with `orOmit()` for incremental resolution.

No cycle at the graph level — the planner resolves the temporal dependency by
splitting the instance node into two steps. The instance node depends on nothing
(for Load) and everything depends on it (for data flow). See Instance Node.

### Lifecycle presets are RGD-facing; CRUD flags are runtime

Users write lifecycle presets in the RGD (`managed`, `read-only`, `orphan`,
etc.). The compiler decomposes each preset into three boolean flags: create,
update, delete. At runtime, no preset names exist — the orchestrator and backend
only see flags. This means the runtime has no lifecycle enum, no string
matching, no preset-awareness. Just three booleans per node.

When lifecycle is omitted, the fallback preset is injected from
controller/runtime Go config (flag), not from RGD schema. Current fallback is
`orphan`.

| Preset (RGD) | create | update | delete |
| ------------ | ------ | ------ | ------ |
| managed      | yes    | yes    | yes    |
| create-only  | yes    | no     | yes    |
| orphan       | yes    | yes    | no     |
| read-only    | no     | no     | no     |
| virtual      | no     | no     | no     |

Read-only vs virtual: both are `{no, no, no}`. The distinction is whether a
cluster resource exists to observe — determined by the template (has GVK or
not), not the lifecycle flags. Observation itself is backend's responsibility,
not a lifecycle concern.

New presets don't require runtime changes — just a new compiler mapping from
name to flags. Custom flag combinations (e.g.,
`{create: yes, update: no, delete: no}`) are possible without inventing a preset
name, though RGD syntax may require one for readability.

The instance node (`lifecycle: instance`) doesn't fit the CRUD model — it uses
`Load` and `Project` ops, not create/update/delete. It's `{no, no, no}` for
cluster IO on child resources. The planner recognizes `lifecycle: instance` and
emits Load/Project steps instead. See Instance Node.

### Partial resolution

Resolve currently operates in strict mode: all CEL expressions must resolve or
error. Some use cases need partial resolution — resolve what you can, omit
fields whose dependencies aren't available yet.

**Use cases:**

- Status sink: RESOLVED — instance node's kernel uses `orOmit()` for incremental
  resolution (see Instance Node)
- Conditional dependencies: gracefully omit fields when an optional
  (conditional) node is skipped
- Progressive aggregation: include partial results from expansion over multiple
  targets

**Possible directions:**

1. **Expression-level sigil** — `${?expr}` means "omit field if unresolvable."
   Per-field opt-in, detected by compiler. Avoids CEL namespace conflicts
   (`optional` is already a CEL type). Resolve kernel stays strict by default;
   `?` expressions are omit-on-failure.

2. **Convention-based** — all status fields are implicitly soft. The compiler
   knows "this is a status expression → soft resolution." No user-facing syntax
   change, but less general.

3. **Compiler splitting** — compiler splits nodes by dependency group so each
   node's expressions are always fully resolvable when it executes. No partial
   resolution needed at runtime. Simpler runtime, more compiler work, more
   synthetic nodes.

4. **kro CEL function** — `${kro.defer(expr)}` or similar namespaced function.
   Explicit, avoids `optional` collision, but adds to kro's CEL surface area.

### Contagious exclusion with branching expressions

Today the compiler infers dependencies by analyzing all CEL references in a
node's template. An expression like
`${schema.spec.useCache ? cache.status.endpoint : database.status.endpoint}`
creates hard edges to both `cache` and `database`, even though only one is
needed at runtime. This is correct for scheduling — the planner should wait for
dependencies before evaluating. But it breaks when combined with conditional
inclusion. If `cache` has an `includeWhen` that evaluates to false, the node is
excluded, and the current model propagates that exclusion to all dependents —
including our node, which never needed `cache` in the first place. The ternary
expression handles both branches cleanly, but the static graph kills the node
before the expression ever gets a chance to evaluate. This isn't an optimization
concern — it's a correctness problem. The user wrote valid logic that the
dependency graph cannot represent.

The core tension is that CEL expressions have runtime branching but the
dependency graph is a single static shape. A ternary creates multiple possible
data flow paths, but the compiler flattens them into hard edges. With one
ternary that's two possible dependency sets. With two ternaries it's four. The
compiler can't emit all possible graph shapes without combinatorial explosion,
and the runtime exclusion model assumes every edge is always needed.

**Approach 1: Lazy exclusion.** The simplest change with no new graph concepts.
Instead of propagating exclusion before resolution, the orchestrator attempts
Resolve first with whatever dependencies are available. If the expression
evaluates successfully — the ternary picked the available branch — the node
proceeds and exclusion never propagates. If resolution fails because a needed
dependency was excluded, exclusion propagates correctly. Same graph, same
planner, just a different evaluation order. The cost is that Resolve runs on
nodes that might ultimately be excluded, but Resolve is pure compute with no IO.
This approach is always correct but gives the compiler no visibility into which
edges are conditional — it defers everything to runtime.

**Approach 2: Tagged edges.** The compiler already parses CEL expressions to
infer dependencies. It can go one step further: when a reference appears inside
a ternary branch, the compiler tags the resulting edge with the condition that
guards it. The edge `A → cache` is tagged with `schema.spec.useCache == true`
and `A → database` with `schema.spec.useCache == false`. The graph structure is
unchanged — edges are static — but each edge carries an activation predicate.
During exclusion propagation, the orchestrator evaluates the tag. If the tag
evaluates to false, the edge is inactive and exclusion does not propagate
through it. This gives the compiler and planner full visibility into conditional
data flow while keeping the graph static. The complexity cost is that the
orchestrator now evaluates predicates on edges, not just on nodes, and deeply
nested ternaries produce compound tag expressions.

**Approach 3: Dependency sets.** The compiler analyzes all branching paths in a
node's expressions and produces multiple dependency sets — each representing one
valid combination of dependencies the node can resolve with. For a single
ternary referencing `cache` or `database`, the node gets two sets:
`{cluster, cache}` and `{cluster, database}`. The planner picks the first
satisfiable set — if `cache` is excluded, that set is unsatisfiable, so it tries
the next. If any set is fully satisfiable, the node proceeds. This models the
problem directly: the node doesn't have one fixed set of dependencies, it has
multiple possible sets and needs any one of them. The combinatorial concern is
real — N ternaries produce up to 2^N sets — but in practice most nodes have zero
or one branching expression, and the compiler can cap or warn on excessive
branching. This approach gives the planner complete information to make
scheduling decisions without runtime edge evaluation.

**Recommendation.** These approaches are not mutually exclusive. Lazy exclusion
is the safety net — cheap, correct, requires no graph changes. Tagged edges or
dependency sets are compiler optimizations on top that give the planner better
information for scheduling and exclusion. A pragmatic path is lazy exclusion for
v1 correctness, with tagged edges or dependency sets as a compiler enhancement
when the scheduling inefficiency matters.

## Open Ideas

### Import — RGDs as functions

RGDs are already functions: schema spec is the input signature, resources are
the body, schema status is the return value. Import makes the function call
explicit.

```yaml
resources:
  - id: database
    import:
      rgd: oci://....
      inputs:
        engine: ${schema.spec.engine}
        size: large

  - id: deployment
    template:
      apiVersion: apps/v1
      kind: Deployment
      spec:
        containers:
          - name: app
            env:
              - name: DB_HOST
                value: ${database.status.endpoint}
```

The compiler resolves `database-rgd`, validates inputs against its schema
(type-check the function call), and inlines the sub-graph's nodes namespaced
under the import ID (`database.deployment`, `database.service`, etc.). After
compilation no Import exists at runtime — it's been expanded to regular nodes.
The runtime sees a flat DAG.

**Encapsulation**: black box by default. Only `database.status.*` is
referenceable from the outer graph. Inner resources are implementation details —
changing them doesn't break consumers.

**Not a runtime kernel.** Import is a compiler construct. The kernel tree and
the orchestrator never see it. It expands before any kernel is built.

**Already possible at runtime** — an RGD can create a CR whose kind is managed
by another RGD. That's runtime composition through the K8s API (two controllers,
two reconcile loops, status-based readiness). Import is the compile-time
alternative: single controller, single reconcile loop, shared observed state,
compile-time type checking.

|                  | Runtime composition           | Compile-time import                       |
| ---------------- | ----------------------------- | ----------------------------------------- |
| New primitive?   | No (already works)            | Yes (compiler feature)                    |
| Validation       | Runtime (status mismatch)     | Compile-time (schema check)               |
| Reconcile loops  | Two controllers               | One controller                            |
| Latency          | Extra reconcile hop           | Direct                                    |
| Independence     | Decoupled, version separately | Coupled, inlined                          |
| Inner visibility | Black box (only status)       | Black box by default, opt-in transparency |

**Implications**: this is a package system. RGDs become reusable, versioned
units. Eventually needs: reference syntax (name? name+version?),
registry/discovery, schema evolution across versions.

## Key Design Properties

- Graphs, nodes, kernels, traits, protocols, planner, orchestrator are the
  runtime model. Compiler, backend, and schema exist outside.
- Kernels shape data (WHAT)
- Nodes are the unit of work; hold desired + observed state (WHO)
- Traits declare instructions for the orchestrator: meta (data) and predicates
  (CEL) (HOW)
- Protocols are lifecycle event + graph + planner (onFailure, onDelete) — not
  traits, not kernels. Protocols are graphs.
- One orchestrator, strategy via planner function (WHEN)
- Planner is pure (graph + state → steps). Orchestrator is a dumb loop (execute
  steps, observe, repeat).
- Three execution strategies (serial, leveled, eager) — a spectrum of
  aggressiveness over the same DAG
- Failure is a predicate (`IsFailed`), not just an IO error. Planner decides the
  response (stop, skip, retry, rollback).
- Protocols are graphs with their own planner — the orchestrator runs them like
  any graph, no special runtime, no state machine
- Instance status is the single source of truth for orchestration state; labels
  are the recovery fallback
- Dependencies are emergent from data flow + explicit DependsOn
- Kernels compose by 1:1 wrapping (function calls), not aggregation — Select is
  the exception (N:1)
- Mutate enables composition without modifying templates
- Nodes have sensible defaults; traits are overrides
- Schema is the function signature, not a runtime concept
- Schema validation is compile-time by default; Validate kernel handles deferred
  cases (compiler-injected)
- Compiler and backend are non-kernel layers
- New capabilities = new kernels, new injected wrapper types, new traits, new
  protocols, new planners, or new backend implementations

## Appendix: Interfaces

Core runtime boundary: planner emits generic ops, runtime wires node + backend,
node hides kernel complexity, backend owns cluster IO details.

### Node + ObservePlan

```go
type ObserveMode string

const (
    ObserveGet  ObserveMode = "Get"
    ObserveList ObserveMode = "List"
    ObserveNone ObserveMode = "None"
)

type ObservePlan struct {
    Mode ObserveMode
    GVKs []schema.GroupVersionKind
}

type Node interface {
    ID() string
    ObservePlan() ObservePlan

    // Compute desired output. Eval behavior is adapted by options.
    GetDesired(opts ...EvalOption) ([]*unstructured.Unstructured, error)

    // Runtime state ingress from backend.
    SetObserved([]*unstructured.Unstructured)

    // Runtime-owned deletion targeting (identity diff/intersection logic).
    DeleteTargets() ([]*unstructured.Unstructured, error)

    // Trait predicates.
    IsIgnored() (bool, error)
    IsReady() (bool, error)
    IsFailed() (bool, error)

    // Graph metadata.
    DependsOn() []string
    Protocols() map[LifecycleEvent]*CompiledGraph
}
```

`ObservePlan` is static and compiler-derived from the compiled kernel tree.
`GetDesired(...)` is the only kernel entrypoint; callers adapt intent with eval
options. Nodes keep desired/observed runtime state internally and build CEL
context from dependency observed state.

### Eval Options (injected wrappers)

```go
type EvalOption interface {
    apply(*evalCfg)
}

func WithIdentityOnly() EvalOption
func WithNormalizedNamespace(ns string) EvalOption
func WithAPIGroupRewrite(rewriteFn any) EvalOption
```

These are runtime-only wrappers (not persisted in compiled graphs). They adapt
one evaluation call without mutating the kernel tree.

### Kernel

```go
type Kernel interface {
    Eval(ctx context.Context, scope map[string]any) ([]*unstructured.Unstructured, error)
}
```

Pure compute only. Six implementations: Template, Resolve, Expansion, Mutate,
Validate, Select.

### Planner + Runtime + Orchestrator

```go
type Op string

const (
    OpLoad      Op = "Load"
    OpReconcile Op = "Reconcile"
    OpProject   Op = "Project"
    OpDelete    Op = "Delete"
)

type Step struct {
    NodeID string
    Op     Op
}

type Planner interface {
    Next(view RuntimeView) ([]Step, bool)
}

type Runtime interface {
    RunStep(ctx context.Context, step Step, node Node, m Backend) error
}

type Orchestrator struct {
    planner   Planner
    runtime  Runtime
    backend Backend
}
```

`Planner` is pure (runtime view in, steps out). `Orchestrator` is a dumb loop
over steps. `Runtime` encapsulates op execution details (identity-only eval,
observe, apply/delete/project, state updates).

### Backend

```go
type Backend interface {
    Observe(
        ctx context.Context,
        instanceUID string,
        nodeID string,
        plan ObservePlan,
        identities []*unstructured.Unstructured,
    ) ([]*unstructured.Unstructured, error)

    Apply(ctx context.Context, desired []*unstructured.Unstructured) error
    Delete(ctx context.Context, targets []*unstructured.Unstructured) error
    PatchStatus(ctx context.Context, instance *unstructured.Unstructured, status map[string]any) error
}
```

Backend is the side-effect boundary. It owns selector construction and label
scheme for LIST observation (using `instanceUID + nodeID`), GET vs LIST
execution, and status patching.

### Runtime Invariants

- `ErrDataPending` is retryable: planner/orchestrator requeues, not a terminal
  failure.
- Collection state is tri-state: unresolved (`nil` desired), resolved-empty
  (`[]` desired), resolved-non-empty.
- Collection observed alignment and identity collision checks are node/runtime
  invariants, not orchestrator policy.

### Summary

|               | Shape                                                   | Polymorphic                          | Stateful      |
| ------------- | ------------------------------------------------------- | ------------------------------------ | ------------- |
| Kernel        | interface (`Eval`)                                      | yes (6 impls)                        | no            |
| CompiledGraph | struct (nodes + strategy)                               | no                                   | no            |
| Node          | interface (state + predicates + plans)                  | no (1 impl target)                   | yes (runtime) |
| Planner       | interface (`Next(view)`)                                | yes (serial, leveled, eager, rollin) | no            |
| Runtime       | interface (`RunStep`)                                   | yes (default, custom)                | no            |
| Orchestrator  | struct (planner + runtime + backend)                    | no (1 impl)                          | no            |
| Backend       | interface (`Observe`, `Apply`, `Delete`, `PatchStatus`) | yes (local, multi-cluster)           | no            |

## Appendix: Compiled RGD Examples

### WorkerPool — forEach expansion

RGD: schema takes `workers: []string` and `image: string`. Single resource
`workerPods` with `forEach` over workers list, producing one Pod per worker.

Compiled output:

```
Node {
    id:      "source-sink"
    kernel:  Resolve(Template({
                 "nodes": {
                     "workerPods":  ${kro.collectionStatus("workerPods").orOmit()}
                 }
             }))
    traits:
        lifecycle:  instance
}

Node {
    id:      "workerPods"
    kernel:  Expansion(schema.spec.workers,
                 Resolve(Template(pod)))
    traits:
        lifecycle:  managed
        dependsOn:  ["source-sink"]    // inferred: refs schema.spec.workers, schema.spec.image, schema.metadata.name
}
```

DAG: `source-sink → workerPods`. Two nodes. Expansion produces N pods at
runtime. The instance node's kernel projects operational status only (RGD has no
user-defined status fields). Planner emits:
`Load(source-sink) → Apply(workerPods) → Project(source-sink)`.

### Application — deployment + service + conditional ingress + status

RGD: schema takes `name`, `image`, `replicas`, `ingress.enabled`. Three
resources: deployment (always), service (depends on deployment for selector),
ingress (conditional on `ingress.enabled`, depends on service). Status projects
`deploymentConditions` and `availableReplicas` from deployment.

Compiled output:

```
Node {
    id:      "source-sink"
    kernel:  Resolve(Template({
                 "deploymentConditions":  ${deployment.status.conditions.orOmit()},
                 "availableReplicas":     ${deployment.status.availableReplicas.orOmit()},
                 "nodes": {
                     "deployment":  ${kro.nodeStatus("deployment").orOmit()},
                     "service":     ${kro.nodeStatus("service").orOmit()},
                     "ingress":     ${kro.nodeStatus("ingress").orOmit()}
                 }
             }))
    traits:
        lifecycle:  instance
}

Node {
    id:      "deployment"
    kernel:  Resolve(Template(deployment))
    traits:
        lifecycle:  managed
        dependsOn:  ["source-sink"]    // inferred: refs schema.spec.name, schema.spec.replicas, schema.spec.image
}

Node {
    id:      "service"
    kernel:  Resolve(Template(service))
    traits:
        lifecycle:  managed
        dependsOn:  ["source-sink", "deployment"]    // inferred: refs schema.spec.name + deployment.spec.selector
}

Node {
    id:      "ingress"
    kernel:  Resolve(Template(ingress))
    traits:
        lifecycle:   managed
        dependsOn:   ["service"]    // inferred: refs service.metadata.name
        condition:   schema.spec.ingress.enabled
}
```

DAG:

```
source-sink → deployment → service → ingress (conditional)
```

Four nodes. Ingress is conditional — skipped when `ingress.enabled` is false,
which also means no orphaned ingress resources. The instance node's kernel
evaluates `deploymentConditions` and `availableReplicas` via the `Project` step
at the end of each cycle — both reference deployment, so they resolve as soon as
deployment has observed state. `orOmit()` handles fields whose dependencies
aren't ready yet.
