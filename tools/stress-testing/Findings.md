# Findings

This file is a running log of performance and stress-testing findings for KRO.

Use each entry to capture:

- the observation
- the likely cause
- the evidence used to support it
- any follow-up worth testing later

## 2026-03-08: Baseline goroutine count is mostly configured worker concurrency

### Observation

With only two active RGDs in the cluster (`appstack.kro.run` and `decksite.kro.run`), the controller still reported about `219` goroutines before any scale test started.

### Finding

This baseline is mostly expected for the current perf-tuned deployment and does not by itself indicate a per-RGD goroutine leak.

The biggest contributors are the preallocated worker pools:

- `80` goroutines from `resourceGraphDefinitionConcurrentReconciles`
- `50` goroutines from `dynamicControllerConcurrentReconciles`

That is roughly `130` goroutines before counting informer, watch, queue, event broadcaster, leader election, metrics, health, and pprof server goroutines.

### Evidence

- Live Helm values set:
  - `resourceGraphDefinitionConcurrentReconciles: 80`
  - `dynamicControllerConcurrentReconciles: 50`
- Live deployment env showed:
  - `KRO_RESOURCE_GROUP_CONCURRENT_RECONCILES=80`
  - `KRO_DYNAMIC_CONTROLLER_CONCURRENT_RECONCILES=50`
- Live goroutine dump grouped by creator showed:
  - `80` goroutines created by the controller-runtime RGD worker pool
  - `50` goroutines created by `DynamicController.Start`
- Live controller metrics showed:
  - `dynamic_controller_gvr_count=2`
  - `dynamic_controller_handler_count_total{type="parent"}=2`

This means the goroutine count was dominated by configured concurrency and controller-runtime infrastructure, not by an unexpectedly large number of registered parent GVRs.

### Implication

If we want a cleaner idle baseline measurement, redeploy with much lower concurrency values, for example:

- `resourceGraphDefinitionConcurrentReconciles=1`
- `dynamicControllerConcurrentReconciles=1`

Then compare goroutine count, CPU, and memory before and after.

## 2026-03-08: The current "5,000 RGD" runner is really a staged 1,000-at-a-time test, and Stage 1 is bottlenecked by apiserver/etcd throttling

### Observation

The `krostress run rgd-scale` experiment was launched with:

- `--total 5000`
- `--step 1000`
- `--rate 25`
- `--complexity high`
- prefix `rgdscale5000hc0308`

Stage 1 creation completed quickly:

- `1000/1000` RGDs created
- `0` create failures
- `40.161s` total create time
- `24.9/sec` observed create rate

The run then spent several minutes waiting for those first 1,000 RGDs to become `Active` before it was allowed to create the next 1,000.

### Finding

This run is not equivalent to the earlier "throw 5,000 RGDs at the cluster" test.

The current runner blocks after each `step` and waits for that entire stage to reach `Active` before it starts the next stage. In practice, this means the active test shape was:

- create 1,000
- wait for 1,000 to reconcile
- only then create the next 1,000

So the current experiment has only exercised Stage 1 so far, not a true all-at-once 5,000-RGD submission.

Within that first stage, the dominant bottleneck is control-plane throttling rather than sustained controller CPU saturation. The controller logs showed repeated:

- `rpc error: code = ResourceExhausted desc = etcdserver: throttle: too many requests`

### Evidence

- Runner behavior in [run.go](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/internal/cli/run.go#L162):
  - loops by `step`
  - creates only `step` RGDs at a time
  - waits for every RGD in the current stage to become `Active`
  - only then continues
- Live process confirmed the running command was:
  - `./bin/krostress run rgd-scale --total 5000 --step 1000 --rate 25 --complexity high ...`
- At `2026-03-08T15:12:25Z`, live cluster state for this run was:
  - `1000` RGDs total
  - `627 Active`
  - `372` with no `status.state` yet
  - `1 Inactive`
- A few minutes later, the same stage had advanced to:
  - `849 Active`
  - `150` with no `status.state` yet
  - `1 Inactive`
- Mid-run Prometheus snapshots during Stage 1 showed:
  - roughly `5.1-5.5 GiB` working set
  - roughly `3.8-4.9 GiB` heap in use
  - roughly `7.5k-7.8k` goroutines
  - `286-312` workqueue depth
  - only `0.08-0.53` CPU cores
- Live controller logs during the same window showed both:
  - successful CRD creation / transition to `Active`
  - repeated `etcdserver: throttle: too many requests` reconciliation failures

### pprof snapshot

Mid-run pprof was captured under:

- [pprof-midrun-20260308-1510](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-20260308-1506/pprof-midrun-20260308-1510)

The hot paths suggest that even when the controller is not CPU-saturated, it is doing expensive JSON/schema/CEL work per RGD.

CPU `pprof -top` highlights:

- `encoding/json.appendCompact`
- `runtime.mallocgcSmallScanNoHeader`
- `github.com/evanphx/json-patch/v5/internal/json.unquoteBytes`
- `github.com/kubernetes-sigs/kro/pkg/cel.buildDeclTypes`

Heap `pprof -top` highlights:

- `github.com/google/cel-go/interpreter.(*defaultDispatcher).Add` at about `1.15 GiB` flat
- `k8s.io/apiserver/pkg/cel.NewSimpleTypeWithMinSize`
- `k8s.io/apiserver/pkg/cel.(*DeclType).MaybeAssignTypeName`
- `k8s.io/apiserver/pkg/cel.NewDeclField`
- `github.com/kubernetes-sigs/kro/pkg/cel.SchemaDeclTypeWithMetadata`

This points to a combination of:

- heavy CEL/schema/type construction
- heavy JSON/object churn
- pressure on the apiserver/etcd path while many CRDs are being created and observed

### Implication

Two different questions need two different test shapes:

- If the goal is "how long does it take to fully reconcile 5,000 RGDs when they are all submitted," the runner needs an all-at-once mode instead of stage gating.
- If the goal is "how does memory/CPU evolve every 1,000 RGDs," the staged mode is useful, but it should be understood as a controlled ramp test, not the historical 5,000-at-once benchmark.

### Follow-up

- Add an all-at-once mode so we can compare:
  - `5000 submitted immediately`
  - `1000-at-a-time with Active gating`
- Investigate why Stage 1 alone can drive:
  - `~5+ GiB` working set
  - `~7.8k` goroutines
  - repeated `etcdserver: throttle: too many requests`
- Focus optimization work first on:
  - CEL type construction and schema declaration building
  - JSON patch / JSON normalization churn
  - CRD creation and readiness polling pressure on the control plane

## 2026-03-08: The controller OOMs at 8 GiB before finishing the first 1,000-RGD stage

### Observation

During the high-complexity scale run, the controller pod was configured with:

- `limits.memory=8000Mi`
- `requests.memory=8000Mi`

The controller still OOMed before the first 1,000-RGD stage fully reconciled.

Live pod state showed:

- last termination reason: `OOMKilled`
- exit code: `137`
- last termination time: `2026-03-08T15:18:05Z`

### Finding

This is not just a control-plane throttling issue. KRO has a real memory-growth problem under high-complexity RGDs, and it is large enough to exhaust an `8 GiB` limit before the first 1,000 RGDs are all `Active`.

The dominant hot paths are in CEL declaration/type construction and CEL program setup, with JSON/object churn contributing additional CPU and allocation pressure.

### Evidence

- Live pod status showed:
  - `OOMKilled`
  - exit code `137`
  - `2` restarts
- Mid-run Prometheus snapshots during the same first-stage experiment showed:
  - roughly `5.1-5.5 GiB` working set
  - roughly `3.8-4.9 GiB` heap in use
  - roughly `7.5k-7.8k` goroutines
- Heap `pprof -top` from [heap-top.txt](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-20260308-1506/pprof-midrun-20260308-1510/heap-top.txt) showed the biggest consumers were:
  - `github.com/google/cel-go/interpreter.(*defaultDispatcher).Add` at about `1.15 GiB` flat
  - `k8s.io/apiserver/pkg/cel.NewSimpleTypeWithMinSize`
  - `k8s.io/apiserver/pkg/cel.(*DeclType).MaybeAssignTypeName`
  - `k8s.io/apiserver/pkg/cel.NewDeclField`
  - `github.com/kubernetes-sigs/kro/pkg/cel.SchemaDeclTypeWithMetadata`
  - `github.com/google/cel-go/cel.newProgram` at about `1.34 GiB` cumulative
- CPU `pprof -top` from [cpu-top.txt](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-20260308-1506/pprof-midrun-20260308-1510/cpu-top.txt) showed:
  - `encoding/json.appendCompact`
  - `runtime.mallocgcSmallScanNoHeader`
  - `github.com/evanphx/json-patch/v5/internal/json.unquoteBytes`
  - `github.com/kubernetes-sigs/kro/pkg/cel.buildDeclTypes`

The most relevant KRO code paths behind these symbols are:

- [environment.go:133](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/environment.go#L133)
- [schemas.go:52](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/schemas.go#L52)
- [types.go:40](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/types.go#L40)
- [builder.go:337](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/graph/builder.go#L337)

### Implication

The first optimization target should be memory retention and repeated allocation in CEL/schema processing, not only reconcile concurrency tuning.

More concretely, the evidence suggests we should investigate:

- repeated rebuilding of declaration types for similar schemas
- repeated CEL provider / environment / program construction
- schema-walk and field-type expansion costs in `SchemaDeclTypeWithMetadata` and `buildDeclTypes`
- JSON normalization and patch churn during graph/resource preparation

### Follow-up

- Add a focused benchmark around:
  - [environment.go:133](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/environment.go#L133)
  - [schemas.go:52](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/schemas.go#L52)
  - [types.go:40](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/types.go#L40)
- Test whether caching or interning CEL declaration/type structures materially reduces heap growth
- Re-run the same first-1,000-RGD workload after any CEL/schema optimization before changing controller concurrency again

## 2026-03-08: High goroutine count during scale-up is mostly parked informer/watch machinery per registered GVR

### Observation

During the first-stage high-complexity run, the controller reported thousands of goroutines:

- mid-run snapshot: about `7779` goroutines
- later snapshot after restart/recovery: about `5118` goroutines

At the later snapshot, the run had:

- `998 Active` RGDs
- `2` RGDs still without `status.state`

### Finding

The large goroutine count is mostly not "active work" goroutines burning CPU. It is primarily parked client-go informer/watch machinery created per registered parent GVR.

## 2026-03-08: The optimized build now reaches 5,000 high-complexity RGDs without restarts, but retained memory and goroutines still scale almost linearly

### Observation

After the build-local CEL environment, decl-type, and program reuse work, KRO was redeployed with:

- image `095708837592.dkr.ecr.us-west-2.amazonaws.com/kro:v0.8.5-main-5-pprof-debug@sha256:3a4e0090f2df532e86a5a1dc51b6a805892282655a18b4e37d3357200595d341`
- controller resources `4 CPU / 16000Mi`

The controller then completed a full staged high-complexity 5,000-RGD run:

- command:
  - `./bin/krostress run rgd-scale --total 5000 --step 1000 --rate 20 --complexity high --prefix high5000schemaenv0308 --output-dir tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046`
- report duration:
  - `11m29s`
- final state:
  - `5000 Active`
  - `0` controller restarts

The experiment was intentionally left running at the end of the run. The cluster was not cleaned up after the 5,000-RGD plateau was reached.

### Finding

The recent builder/CEL reuse optimizations materially improved survivability: KRO now reaches `5000 Active` high-complexity RGDs where earlier builds OOMed far earlier.

However, the steady-state retained cost is still very large and still scales roughly linearly with active RGDs:

- around `1000` RGDs:
  - `1.68 GiB` working set
  - `1.60 GiB` heap in use
  - `12.2k` goroutines
- around `2000` RGDs:
  - `3.33 GiB` working set
  - `2.64 GiB` heap in use
  - `24.2k` goroutines
- around `3000` RGDs:
  - `4.91 GiB` working set
  - `3.74 GiB` heap in use
  - `36.2k` goroutines
- around `4000` RGDs:
  - `6.58 GiB` working set
  - `5.65 GiB` heap in use
  - `48.3k` goroutines
- around `5000` RGDs:
  - `7.96 GiB` working set immediately after activation
  - `6.56 GiB` heap in use immediately after activation
  - `60.2k` goroutines

The hold-state sample a few seconds later, after work drained, still showed:

- `7.96 GiB` working set
- `6.60 GiB` heap in use
- `60.3k` goroutines
- `0` active workers
- `0` workqueue depth

That means the remaining problem is not just transient activation churn. A large amount of state remains live after all `5000` RGDs are fully `Active`.

### Evidence

- Full run report:
  - [report.html](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/report.html)
- Full summary:
  - [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/summary.json)
- Final built-in profiles:
  - [cpu-20260308-175546.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/profiles/cpu-20260308-175546.prof)
  - [heap-20260308-175710.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/profiles/heap-20260308-175710.prof)
- Extra live profiles:
  - [pprof-stage1-extra](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/pprof-stage1-extra)
  - [pprof-stage2-extra](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/pprof-stage2-extra)
  - [pprof-stage4-extra](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/pprof-stage4-extra)
  - [pprof-stage5-hold](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/pprof-stage5-hold)

Key run summary numbers from [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/summary.json):

- duration:
  - `11m29s`
- max CPU:
  - `0.724` cores
- max working set:
  - `8,547,160,064` bytes, about `7.96 GiB`
- max resident memory:
  - `8,557,662,208` bytes, about `7.97 GiB`
- max heap in use:
  - `7,719,714,816` bytes, about `7.19 GiB`
- max goroutines:
  - `60,265`
- max workqueue depth:
  - `498`
- reconcile errors:
  - `0`
- restarts:
  - `0`

The final heap hot paths shifted compared with the earlier pre-optimization OOM profiles:

- `github.com/google/cel-go/interpreter.(*defaultDispatcher).Add` still dominates retained heap at about `1.45 GiB flat`
- `github.com/google/cel-go/cel.newProgram` is still large at about `1.74 GiB cumulative`
- `github.com/kubernetes-sigs/kro/pkg/graph.(*Builder).buildRGResource` is now visible at about `0.95 GiB cumulative`
- JSON decode churn is also prominent:
  - `encoding/json.(*decodeState).objectInterface`
  - `k8s.io/apimachinery/pkg/runtime.(*RawExtension).UnmarshalJSON`
- the earlier catastrophic type-declaration buckets are much smaller now:
  - `MaybeAssignTypeName` about `0.11 GiB cumulative`
  - `NewDeclField` about `0.06 GiB`

The final CPU profile is now dominated more by GC/scan pressure than by one obvious KRO-specific compute hotspot:

- `runtime.scanobject`
- `runtime.findObject`
- `runtime.gcDrain`
- plus smaller JSON validation/decode paths

### Implication

The recent optimizations fixed an important class of repeated build-time work, but they did not change the basic linear retained-state story for active RGDs.

At this point:

- KRO can survive a 5,000-RGD high-complexity run with a `16 GiB` memory limit
- but it still settles near `8 GiB` working set and `~60k` goroutines at `5000 Active`
- and the remaining retained heap is still strongly dominated by compiled CEL program / interpreter state, plus graph-build and JSON/object materialization costs

This is consistent with the earlier conclusion that the next major flattening step is not more schema pointer sharing. It is reducing what gets retained per active RGD after activation completes.

### Follow-up

- Investigate whether compiled CEL programs can be shared across RGDs, not just within one build, when the expression text and typed environment signature match
- Investigate what `buildRGResource` is retaining per active RGD and whether that object graph can be made smaller
- Measure how much of the remaining `~60k` goroutine steady state is strictly one-watch-per-GVR overhead
- Keep this experiment alive long enough to confirm whether steady-state memory plateaus further or stays flat near the `~8 GiB` band

## 2026-03-08: The `~60k` goroutines at `5000 Active` are mostly parked informer/watch stacks, not active reconciliation work

### Observation

At the `5000 Active` hold point, KRO reported about:

- `60,261` goroutines in the extra hold-state sample
- `0` active workers
- `0` workqueue depth
- `0` controller restarts

That raised the obvious question of whether KRO was actually still doing massive active work after reconciliation had finished.

### Finding

The answer is mostly no.

The hold-state goroutine profile shows that the `~60k` is dominated by parked client-go informer/watch machinery, repeated almost exactly once per generated parent GVR. Since this test created `5000` unique generated kinds, the current architecture ended up with roughly `5000` informer/watch pipelines alive at once.

In other words, the goroutine count is mostly a scaling property of the "one generated GVR, one parent watch/informer" design, not evidence that `60000` reconciles were actively running.

### Evidence

Hold-state goroutine profile:

- [goroutine-20260308-175713.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/pprof-stage5-hold/goroutine-20260308-175713.prof)

`go tool pprof -top` on that profile showed:

- `60260` goroutines parked in `runtime.gopark`
- `5001` in `k8s.io/client-go/tools/cache.(*Reflector).ListAndWatchWithContext`
- `5002` in `k8s.io/client-go/tools/cache.(*controller).processLoop`
- `5000` in `k8s.io/client-go/tools/cache.(*sharedIndexInformer).Run`
- `4999` in HTTP2/watch read/decode paths:
  - `golang.org/x/net/http2.transportResponseBody.Read`
  - `k8s.io/apimachinery/pkg/watch.(*StreamWatcher).receive`
  - `k8s.io/client-go/rest/watch.(*Decoder).Decode`
- `10002` in `k8s.io/client-go/tools/cache.(*processorListener).pop`
- `10002` in `k8s.io/client-go/tools/cache.(*processorListener).run`
- `35009` in `k8s.io/apimachinery/pkg/util/wait.(*Group).Start.func1`

Those repeated counts line up with the architecture in:

- [dynamic_controller.go](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/dynamiccontroller/dynamic_controller.go)
- [watch.go](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/dynamiccontroller/watch.go)

Each generated RGD kind results in a distinct parent GVR watch. That watch brings along the usual client-go shared informer stack:

- reflector
- watch stream reader / decoder
- controller process loop
- shared informer run loop
- processor listeners
- wait-group helper goroutines

That gets you to roughly `~12` goroutines per generated GVR, which is how `5000` active RGDs turn into `~60k` goroutines even when workqueues are idle.

### Implication

This is not a classic goroutine leak in the sense of runaway active execution. It is retained watch/informer overhead.

That means reducing peak goroutine count materially will require architectural change, not just reconcile tuning. The most likely lever is reducing the number of long-lived watches/informers required per active generated kind.

## 2026-03-08: `GOEXPERIMENT=greenteagc` did not materially improve the 5,000-RGD high-complexity result

### Observation

The same staged `5000`-RGD `high` run was repeated against a controller built with:

- `GOEXPERIMENT=greenteagc`
- image `095708837592.dkr.ecr.us-west-2.amazonaws.com/kro:v0.8.5-main-6-greenteagc-pprof-debug@sha256:9498f03a419571413cdfd09fbeb6b59c81953d607c4eeb78ee9e4777628b5bf2`
- the same deployed resources as the prior comparison:
  - `4 CPU / 16000Mi`

Artifacts for the greenteagc run:

- [report.html](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/report.html)
- [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/summary.json)
- [pprof-stage2-extra](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/pprof-stage2-extra)
- [pprof-stage4-extra](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/pprof-stage4-extra)
- [pprof-stage5-hold](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/pprof-stage5-hold)

### Finding

`greenteagc` did not produce a clear end-to-end win on this workload.

Compared with the prior non-greentea optimized build, the greenteagc run:

- finished a bit slower:
  - `12m18s` vs `11m29s`
- had slightly worse peak memory:
  - max working set `8.00 GiB` vs `7.96 GiB`
  - max heap in use `7.48 GiB` vs `7.19 GiB`
- had essentially identical goroutine growth:
  - max goroutines `60,275` vs `60,265`
- had slightly lower max observed CPU:
  - `0.67` cores vs `0.72`
- had slightly higher max workqueue depth:
  - `528` vs `498`

At the end-of-run `5000` stage snapshot, the two builds were effectively the same:

- greenteagc:
  - `8.00 GiB` working set
  - `6.59 GiB` heap in use
  - `60,275` goroutines
- previous build:
  - `7.96 GiB` working set
  - `6.56 GiB` heap in use
  - `60,247` goroutines

The only mildly favorable signal for greenteagc was the extra post-run hold sample. After the queue had fully drained, the hold-state sample showed:

- working set about `7.48 GiB` last / `8.00 GiB` max over the 20s hold window
- heap in use about `5.96 GiB`
- `60,276` goroutines
- `0` active workers
- `0` workqueue depth

That is somewhat lower retained heap than the prior build's hold sample, but not enough to claim a decisive improvement in the overall workload. The peak memory and total runtime were not better.

### Evidence

Key greenteagc summary numbers from [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/summary.json):

- duration:
  - `12m18s`
- max CPU:
  - `0.671`
- max working set:
  - `8,587,272,192` bytes, about `8.00 GiB`
- max resident memory:
  - `8,598,675,456` bytes, about `8.01 GiB`
- max heap in use:
  - `8,036,810,752` bytes, about `7.48 GiB`
- max goroutines:
  - `60,275`
- max workqueue depth:
  - `528`
- reconcile errors:
  - `0`
- restarts:
  - `0`

The extra hold sample at `5000 Active` from [observation-20260308-182023.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/observation-20260308-182023.json) showed:

- CPU cores:
  - avg `0.07`, max `0.10`
- working set:
  - avg `7.69 GiB`, max `8.00 GiB`, last `7.48 GiB`
- resident memory:
  - avg/max `7.49 GiB`
- heap in use:
  - avg `5.95 GiB`, max `5.96 GiB`, last `5.96 GiB`
- goroutines:
  - avg `60,276.8`, max `60,278`
- active workers:
  - `0`
- workqueue depth:
  - `0`

The final heap and goroutine profiles look effectively like the previous build:

- hold-state heap top:
  - `github.com/google/cel-go/interpreter.(*defaultDispatcher).Add` still dominates at about `1.54 GiB flat`
  - `github.com/google/cel-go/cel.newProgram` is still about `1.88 GiB cumulative`
  - `github.com/kubernetes-sigs/kro/pkg/graph.(*Builder).buildRGResource` is still about `1.04 GiB cumulative`
  - JSON decode / `RawExtension.UnmarshalJSON` are still prominent
- hold-state goroutine top:
  - still about `5000` reflector/watch stacks
  - still about `10000` processor listener goroutines
  - still about `35010` wait-group helper goroutines
  - total still about `60k`, all parked in `runtime.gopark`

### Implication

For this specific workload, `greenteagc` does not change the main scaling story:

- retained CEL program/interpreter state is still the biggest heap bucket
- `buildRGResource` and JSON/object decode are still heavy
- one-watch-per-generated-GVR still drives goroutine growth

So if the goal is meaningfully better 5,000-RGD scale behavior, the next wins still look architectural rather than GC-flag-driven.

The dynamic controller creates and keeps one metadata informer per watched GVR in [watch.go:83](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/dynamiccontroller/watch.go#L83), starting it in [watch.go:94](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/dynamiccontroller/watch.go#L94). Parent watches are attached from [dynamic_controller.go:339](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/dynamiccontroller/dynamic_controller.go#L339).

Because this test creates a distinct CRD/GVR for each generated RGD, the number of active watches grows roughly with the number of reconciled RGDs.

### Evidence

- Goroutine `pprof -top` from the mid-run snapshot showed:
  - `7776/7779` goroutines parked in `runtime.gopark`
  - `4431` in `k8s.io/apimachinery/pkg/util/wait.(*Group).Start.func1`
  - `633` in `k8s.io/client-go/tools/cache.(*Reflector).ListAndWatchWithContext`
  - `634` in `k8s.io/client-go/tools/cache.(*controller).processLoop`
  - `1265` in `k8s.io/client-go/tools/cache.(*processorListener).pop`
  - `633` in HTTP2/watch decode paths:
    - `golang.org/x/net/http2.transportResponseBody.Read`
    - `k8s.io/apimachinery/pkg/watch.(*StreamWatcher).receive`
    - `k8s.io/client-go/rest/watch.(*Decoder).Decode`
- Those repeated counts around `633` line up with the number of registered watches at the time of the mid-run snapshot.
- Live controller logs repeatedly showed:
  - `Informer started`
  - `Attached parent watch`
  - `Successfully registered GVR`
- Baseline configured worker pools are still present, but small relative to the scale-run totals:
  - `80` RGD controller workers
  - `50` dynamic controller workers

### Interpretation

The goroutine count is scaling largely with "how many distinct parent GVR watches exist", not just with "how many reconcile workers are configured".

An approximate mental model from this run is:

- baseline controller/runtime overhead: a few hundred goroutines
- plus on the order of `5-8` goroutines per registered watched GVR

That is why the goroutine count jumped from about `219` at idle to the `5k-7.8k` range once hundreds of unique RGD-derived CRDs were registered.

### Implication

This looks more like a scaling property of the current watch architecture than a classic goroutine leak.

The real problem is that one-RGD-one-GVR-watch causes:

- goroutine growth
- watch/informer growth
- more pressure on memory and the apiserver/watch stack

even when most of those goroutines are parked.

### Follow-up

- Add a metric for active parent watches directly to reports so it can be graphed alongside `go_goroutines`
- Evaluate whether multiple generated resources can share fewer watches instead of one informer per RGD-derived GVR
- Correlate:
  - active watch count
  - goroutine count
  - heap growth
  - apiserver throttle events

## 2026-03-08: A simpler deployment-heavy RGD shape still OOMs during RGD activation alone

### Observation

A new deployment-heavy stress preset was added that defines a 100-resource graph per RGD:

- `50` `ConfigMap` templates
- `50` zero-replica `Deployment` templates
- each `Deployment` references exactly one generated `ConfigMap`

This run was launched with:

- `./bin/krostress run rgd-scale --total 500 --step 100 --rate 20 --complexity deployments --prefix deploypair500-0308 ...`

No instances were created during this experiment. The runner only created RGDs and waited for them to become `Active`.

### Finding

This is a more serious signal than the earlier high-complexity test shape. Even with a simpler graph made of standard Kubernetes resources and no instance creation, KRO still OOMed during Stage 1 while trying to activate only the first `100` RGDs.

That means the memory problem is not limited to:

- the older CEL-heavy `ConfigMap` / `ServiceAccount` / `Role` / `RoleBinding` mix
- instance fanout
- live object materialization from generated CRs

The controller can exhaust memory while processing RGD activation itself.

### Evidence

- Stage 1 create completed cleanly:
  - `100/100` RGDs created
  - `0` failures
  - about `19.9/sec`
  - about `5.032s` total create time
- During the following `Active` wait:
  - only `2/100` RGDs reached `Active`
  - `98/100` still had no `status.state`
- Live Prometheus snapshot during the stall showed:
  - about `4.34 GiB` controller working set
- The controller pod then restarted again:
  - previous restart count: `6`
  - last termination reason: `OOMKilled`
- After restart, the run still showed:
  - `2 Active`
  - `98` without `status.state`
  - no forward progress
- The run was then canceled and the generated RGDs/CRDs were cleaned up.

The deployment-heavy generator itself lives in:

- [workload.go:27](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/internal/stress/workload.go#L27)
- [workload.go:173](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/internal/stress/workload.go#L173)

### Interpretation

This narrows the problem further:

- KRO does not need instances to hit pathological memory growth
- KRO does not need `Role` / `RoleBinding` fanout to hit pathological memory growth
- activating many distinct RGDs with distinct generated kinds is enough

So the critical path to investigate is the cost of RGD ingestion and activation itself:

- schema processing
- graph building
- generated CRD preparation and publication
- dynamic-controller registration per generated kind

### Follow-up

- Capture a heap and CPU profile specifically during this deployment-heavy RGD-only activation path
- Compare memory growth at:
  - `25`
  - `50`
  - `100`
  deployment-heavy RGDs
- Diff hot paths between:
  - `high`
  - `deployments`
  complexity presets
- Verify whether the main retained heap is still dominated by CEL/schema/type construction, or whether CRD / graph / dynamic-controller registration now dominates

## 2026-03-08: Deployment-heavy RGD-only pprof points at graph-build CEL/schema expansion as the root memory sink

### Observation

After the deployment-heavy `RGD-only` repro stalled and OOMed during the first `100` RGDs, a focused pprof capture was taken from a smaller live repro:

- `./bin/krostress stress rgd create --total 100 --rate 20 --complexity deployments --prefix deploypair100-pprof-0308`
- followed by:
- `./bin/krostress pprof collect all --output tools/stress-testing/results/profiles/deploypair100-pprof-0308 --seconds 20`

No instances were created in this capture. The controller was only ingesting and activating RGDs.

### Finding

The explosion is happening inside the graph-build and CEL typing path itself, before instance fanout and before watch/informer growth becomes the dominant issue.

The retained heap is dominated by OpenAPI-to-CEL declaration building for resource schemas, especially for large built-in schemas like `apps/v1 Deployment`. The controller is repeatedly walking schema properties, constructing `DeclType` trees, assigning nested type names, and building CEL environments/programs during RGD activation.

### Evidence

- Heap profile:
  - [heap-20260308-161144.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/profiles/deploypair100-pprof-0308/heap-20260308-161144.prof)
- Allocation profile:
  - [allocs-20260308-161144.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/profiles/deploypair100-pprof-0308/allocs-20260308-161144.prof)
- CPU profile:
  - [cpu-20260308-161205.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/profiles/deploypair100-pprof-0308/cpu-20260308-161205.prof)
- Goroutine profile:
  - [goroutine-20260308-161144.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/profiles/deploypair100-pprof-0308/goroutine-20260308-161144.prof)

Heap `pprof -top` highlights:

- `k8s.io/apiserver/pkg/cel/openapi.(*Schema).Properties` at about `310 MB` flat
- `k8s.io/apiserver/pkg/cel.(*DeclType).MaybeAssignTypeName` at about `297 MB` flat
- `k8s.io/apiserver/pkg/cel.NewDeclField` at about `164 MB` flat
- `github.com/kubernetes-sigs/kro/pkg/cel.SchemaDeclTypeWithMetadata` at about `161 MB` flat and about `1.03 GB` cumulative
- `github.com/kubernetes-sigs/kro/pkg/cel.DefaultEnvironment` at about `1.49 GB` cumulative
- `github.com/kubernetes-sigs/kro/pkg/graph.(*Builder).NewResourceGraphDefinition` at about `1.57 GB` cumulative

Allocation `pprof -top` highlights:

- `k8s.io/apiserver/pkg/cel/openapi.(*Schema).Properties` at about `2.87 GB` alloc space
- `github.com/kubernetes-sigs/kro/pkg/cel.SchemaDeclTypeWithMetadata` at about `3.62 GB` cumulative alloc space
- `fmt.Sprintf` at about `249 MB` alloc space
- `github.com/kubernetes-sigs/kro/pkg/graph.(*Builder).buildRGResource` at about `619 MB` cumulative alloc space

CPU `pprof -top` highlights:

- `runtime.scanobject`
- `runtime.mallocgc`
- `runtime.findObject`
- `github.com/kubernetes-sigs/kro/pkg/cel.buildDeclTypes`
- `k8s.io/apiserver/pkg/cel.(*DeclType).MaybeAssignTypeName`

The most relevant code paths behind this profile are:

- [builder.go:141](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/graph/builder.go#L141)
- [builder.go:257](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/graph/builder.go#L257)
- [builder.go:261](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/graph/builder.go#L261)
- [builder.go:992](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/graph/builder.go#L992)
- [environment.go:133](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/environment.go#L133)
- [environment.go:196](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/environment.go#L196)
- [schemas.go:52](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/schemas.go#L52)
- [types.go:40](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/pkg/cel/types.go#L40)

### Interpretation

This is not primarily a `Deployment` runtime problem. It is a builder-time schema expansion problem.

The current path appears to pay for the same heavy schema many times:

- once per resource node in the RGD
- again when building the typed CEL environment
- again when building the decl-type provider
- again when deriving expected field types for template validation
- again on later reconciles because the graph is rebuilt from scratch

With deployment-heavy RGDs, this becomes pathological because many resources share the same very large `Deployment` schema, but the builder still expands it repeatedly.

### Follow-up

- Cache declaration/type construction by schema identity instead of rebuilding per node
- Reuse one build artifact for both the typed CEL environment and the decl-type provider
- Cache processed graphs by RGD content or generation so retries do not rebuild the whole graph
- Re-run the deployment-heavy `100`-RGD repro after builder-side caching lands and compare:
  - heap in use
  - alloc space
  - time to `Active`

## 2026-03-08: Reusing one CEL type family per GVR inside a build removes the immediate deployment-heavy OOM at 100 RGDs

### Observation

A builder-only optimization was added so identical resources in the same build share one CEL type family per `GVR`, instead of registering the same schema repeatedly per resource ID.

The key behavior change was:

- variables still keep their resource IDs in CEL
- type roots are now shared per `GroupVersionResource`
- collection variables use a shared list root per `GroupVersionResource`
- expected field types now come from the already-built decl-type provider instead of re-converting sub-schemas during validation

The deployment-heavy repro was rerun with:

- image: `095708837592.dkr.ecr.us-west-2.amazonaws.com/kro:v0.8.5-main-3-pprof-debug`
- command:
  - `./bin/krostress run rgd-scale --total 100 --step 100 --rate 20 --complexity deployments --prefix deploypair100-gvr-0308 --observe-interval 10s --post-stage-pause 15s --wait-timeout 20m --cpu-profile-seconds 30 --output-dir tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948`

### Finding

This change materially improved the builder path.

The controller no longer OOMed on the first `100` deployment-heavy, `RGD-only` resources. All `100` reached `Active`, there were `0` restarts, and retained memory dropped from the prior multi-gigabyte failure mode to roughly `1 GiB` working set / `0.89 GiB` heap in this run.

This confirms a large part of the earlier explosion was duplicate type-family construction for repeated identical resources.

### Evidence

- Report bundle:
  - [report.html](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/report.html)
  - [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/summary.json)
- Mid-run profiles:
  - [heap-20260308-164832.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/pprof-midrun/heap-20260308-164832.prof)
  - [allocs-20260308-164832.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/pprof-midrun/allocs-20260308-164832.prof)
  - [goroutine-20260308-164832.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/pprof-midrun/goroutine-20260308-164832.prof)
- End-of-run profiles:
  - [cpu-20260308-164845.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/profiles/cpu-20260308-164845.prof)
  - [heap-20260308-164848.prof](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/profiles/heap-20260308-164848.prof)

Run summary:

- wall time: about `34s`
- create phase: `100` RGDs in about `5.0s`
- activation: `100/100 Active`
- controller restarts: `0`
- peak working set: about `1.03 GiB`
- peak heap in use: about `887.9 MiB`
- peak resident memory: about `1.05 GiB`
- peak goroutines: `1399`

Symbolized heap `pprof -top` highlights:

- `github.com/google/cel-go/interpreter.(*defaultDispatcher).Add` at about `220 MB` flat
- `k8s.io/apiserver/pkg/cel.(*DeclType).MaybeAssignTypeName` at about `14.5 MB` flat
- `github.com/kubernetes-sigs/kro/pkg/cel.SchemaDeclTypeWithMetadata` at about `37 MB` cumulative
- `github.com/kubernetes-sigs/kro/pkg/graph.(*declTypeBuildCache).namedDeclType` at about `57 MB` cumulative
- `github.com/kubernetes-sigs/kro/pkg/graph.buildTypedEnvironmentWithCache` at about `63.6 MB` cumulative
- `github.com/google/cel-go/cel.newProgram` still dominates cumulative retained heap at about `255.8 MB`

Symbolized CPU `pprof -top` highlights:

- runtime allocation / GC work still dominates overall samples
- KRO-specific cumulative CPU is now concentrated in:
  - `github.com/kubernetes-sigs/kro/pkg/graph.parseCheckAndCompile`
  - `github.com/kubernetes-sigs/kro/pkg/graph.validateAndCompileTemplates`
  - `github.com/kubernetes-sigs/kro/pkg/graph.buildDependencyGraph`
  - `github.com/google/cel-go/parser.(*Parser).Parse`
  - `github.com/google/cel-go/cel.newProgram`
- `github.com/kubernetes-sigs/kro/pkg/graph.(*declTypeBuildCache).namedDeclType` is visible, but much smaller than the earlier repeated schema/type-name assignment path

Mid-run goroutine profile:

- `1397` total goroutines
- `1394` parked in `runtime.gopark`
- repeated informer/watch groups at about `102` each
- about `710` in `wait.(*Group).Start.func1`

So this optimization fixed the builder-time memory cliff, but it did not change the watch/informer scaling shape.

### Interpretation

The result is strong evidence that duplicate per-resource type-family registration was one of the main causes of the earlier memory blow-up.

The remaining dominant costs are now:

- CEL program construction
- CEL parsing / type checking per expression
- general JSON / YAML / patch churn in reconciliation
- informer/watch goroutine growth once many generated kinds are active

The important contrast with the earlier deployment-heavy run is that `MaybeAssignTypeName` is no longer the catastrophic retained-heap bucket it was before. It is still present, but it is no longer the primary memory disaster.

### Follow-up

- Compare this `100`-RGD result against `250` and `500` deployment-heavy RGDs with the same image
- Attack the next builder hot path:
  - `parseCheckAndCompile`
  - repeated CEL parser/program setup
- Consider whether repeated identical expressions can share compiled artifacts inside one build
- Keep tracking goroutine growth separately, since the watch model still scales roughly with managed kinds
