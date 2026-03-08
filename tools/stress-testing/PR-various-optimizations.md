# COMMIT TITLE

Reduce duplicate graph-builder and CEL work for large-RGD scale

# PR DESCRIPTION

This PR reduces repeated graph-builder and CEL work while activating large numbers of unique `ResourceGraphDefinition`s. The core changes are all build-local: we now reuse declaration/type construction inside a build, share one CEL type family per `GroupVersionResource`, reuse compiled CEL programs when the expression source and typed environment match, reuse a shared base CEL environment, and avoid rewalking identical OpenAPI schemas when a raw decl tree can be reused.

We got to these fixes by isolating each failure mode with purpose-built stress runs and pprof. A deployment-heavy `RGD-only` repro with `50` `Deployment`s and `50` `ConfigMap`s per RGD still OOMed during the first `100` RGDs, which told us the first problem was RGD activation itself, not instance fanout. pprof on that repro pointed directly at `SchemaDeclTypeWithMetadata`, `MaybeAssignTypeName`, and `DefaultEnvironment`, so we attacked duplicated schema/type construction first. That changed the `deployments/100` repro from a multi-gigabyte near-failure into a clean `34s` run with about `1.03 GB` working set and about `0.89 GB` heap.

Once the type-declaration explosion was under control, the next bottleneck was compiled CEL program retention. On the `high/1000` benchmark, reusing compiled programs cut peak working set from about `4.62 GB` to about `1.75 GB` and peak heap from about `4.06 GB` to about `1.62 GB`. With the later env/schema cleanups on top, KRO reached a clean `5000/5000 Active` run in `11m29s` with `0` restarts. This does not solve steady-state scaling yet: the successful `5000`-RGD run still peaked around `8.55 GB` working set, `7.72 GB` heap, and about `60k` mostly parked informer/watch goroutines.

One important clarification from the profiling work: the goroutine count is high, but it is normal for the current architecture. The baseline idle count was already around `219`, mostly from configured worker pools and controller-runtime plumbing. At `5000 Active`, the `~60k` goroutines were not `60k` active reconciles or a classic runaway goroutine leak. They were mostly parked client-go informer/watch stacks created by the current one-generated-parent-`GVR` to one-watch-pipeline model, which works out to roughly `~12` goroutines per active generated `GVR`.

# HOW WE HUNTED THIS DOWN

1. We started with the biggest realistic workload we had: the original `high` benchmark at `5000` RGDs. That graph shape is the KRO-heavy mix of `25` `ConfigMap`, `25` `ServiceAccount`, `25` `Role`, and `25` `RoleBinding` templates per RGD. At first, the run looked like it might just be an EKS control-plane problem because we were seeing etcd throttling and low controller CPU. But the controller then `OOMKilled` itself at `8 GiB` before the first `1000`-RGD stage even finished. That was the turning point: the control plane was noisy, but we also clearly had a KRO memory problem. Reference: [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L170).

2. The next question was whether the memory growth came from "real" runtime behavior such as instance fanout, RBAC churn, and watch registration, or whether we were already broken much earlier during RGD activation. To answer that, we built a harsher but simpler debug workload: `50` zero-replica `Deployment`s and `50` `ConfigMap`s per RGD, with no instances at all. That workload still OOMed while processing just the first `100` RGDs, and only `2/100` became `Active`. That was exactly the result we needed, because it ruled out instance fanout as the primary explanation and told us to look directly at the graph-builder path. Reference: [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L635).

3. Once we had that narrower repro, pprof made the first bug obvious: we were repeatedly rebuilding CEL declaration/type state from the same schemas. The heap was dominated by `SchemaDeclTypeWithMetadata`, `MaybeAssignTypeName`, `NewDeclField`, and `DefaultEnvironment`, especially on large built-in schemas like `Deployment`. That led to the first round of fixes: build-local decl/type reuse, then one CEL type family per `GVR`. After those landed, the deployment-heavy repro stopped falling over immediately and became fast enough to use as a tight regression test. Reference: [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L718).

4. After that, we switched back to the original `high` benchmark, because the deployment-heavy case had done its job as a debugging scalpel. The RBAC/ConfigMap/SA mix was still the better benchmark for judging whether KRO itself was getting better on the workloads we actually care about. On that original shape, the next pprof wall was no longer schema typing; it was compiled CEL program retention. That is why the next fix was build-local program reuse keyed by expression text and typed environment.

5. With program reuse in place, the `high/1000` benchmark dropped from about `4.62 GB` working set and `4.06 GB` heap to about `1.75 GB` working set and `1.62 GB` heap. That gave us confidence that we were now removing real duplicated work rather than just moving the pressure around. We then stacked the base-env and schema fast-path cleanups on top and went back to the full `high/5000` run.

6. The final `high/5000` validation was the first clean `5000/5000 Active` run with `0` restarts. That told us the builder/CEL duplication problem had been cut down enough for KRO to survive. It also exposed the next limit clearly: even after the builder fixes, the controller still settles near `8.5 GB` working set and about `60k` goroutines because active RGDs keep real runtime state alive and the current one-generated-parent-`GVR` to one-watch-pipeline architecture is inherently expensive. References: [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L261), [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L393).

# EARLY QUICK-OOM MEMORY VIEWS

These were the initial failure signals that drove the investigation. They are shown as memory-only Grafana panels so the reader can focus on the actual failure mode instead of the rest of the dashboard noise.

### Original `high` quick-OOM window

This is the pre-optimization controller behavior from `08:00` to `08:30` America/Los_Angeles.

Workload:

- benchmark shape: original `high`
- total target: `5000` RGDs
- staging: `1000` RGDs at a time
- per-RGD shape: `25` `ConfigMap` + `25` `ServiceAccount` + `25` `Role` + `25` `RoleBinding`
- pod memory limit at the time: `8000Mi`

![Memory panel for the original quick-OOM period from 08:00 to 08:30](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-memory-20260308-0800-0830.png)

During this window, Prometheus shows the controller restart count climbing from `0` to `5`, and `last_terminated_reason{reason="OOMKilled"}` was present during the crash period. This experiment did not even finish the first `1000`-RGD stage before running out of memory.

### Deployment-heavy quick-OOM window

This is the fast-fail deployment-heavy repro captured from `09:05` to `09:14` America/Los_Angeles.

Workload:

- benchmark shape: deployment-heavy `RGD-only`
- command shape: `--total 500 --step 100`
- per-RGD shape: `50` zero-replica `Deployment` templates + `50` `ConfigMap` templates
- instances created: none
- reason for this repro: isolate RGD activation cost from instance fanout and RBAC-specific churn

![Memory panel for the deployment-heavy quick-OOM period from 09:05 to 09:14](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-memory-20260308-0905-0914.png)

This run is the `deploypair500` / `rgd-scale-500-deployments-20260308-1634` failure. It did not write a `summary.json`, so this window is anchored from the restart spikes and the matching findings entry rather than from a result bundle timestamp. In that repro, KRO OOMed while processing just the first `100` RGDs and only `2/100` became `Active`. Prometheus shows restart jumps during this window at about `09:07:30`, `09:09:00`, and `09:13:00` PDT.

### Why we switched back to the RBAC/ConfigMap/SA benchmark

The deployment-heavy repro was a debugging workload, not the final comparison workload. Once it told us the initial explosion was in graph/CEL build-time work, we switched back to the original `high` preset so the rest of the optimization work could be measured against the more representative KRO graph:

- `25` `ConfigMap`
- `25` `ServiceAccount`
- `25` `Role`
- `25` `RoleBinding`

That let us compare the fixes against the same family of runs all the way from `100` RGDs to `1000` RGDs to `5000` RGDs.

# BEFORE/AFTER BY FIX

The Grafana screenshots below were generated from the exact `startedAt` / `endedAt` timestamps recorded in each run's `summary.json`, with a small pad on both sides. They are tied to the benchmark artifacts, not hand-picked dashboard windows.

### 1. Share one CEL type family per `GVR` inside a build

Benchmark: `deployments`, `100` RGDs, RGD-only

| Before                                                                                                                                                                                                            | After                                                                                                                                                                                                       |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| build-local decl reuse only                                                                                                                                                                                       | per-`GVR` type-family reuse                                                                                                                                                                                 |
| ![Before per-GVR type-family reuse](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-deployments-100-buildercache-memory.png) | ![After per-GVR type-family reuse](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-deployments-100-gvrroot-memory.png) |

Result:

- duration: `1m7s` -> `34s`
- peak working set: `5.22 GB` -> `1.03 GB`
- peak heap in use: `3.96 GB` -> `0.89 GB`

References:

- [before summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-buildercache-20260308-0930/summary.json)
- [after summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-100-deployments-gvrroot-20260308-0948/summary.json)

### 2. Reuse compiled CEL programs when expression text and typed env match

Benchmark: original `high` shape, `1000` RGDs

| Before                                                                                                                                                                                     | After                                                                                                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| per-`GVR` type-family reuse, but no program cache                                                                                                                                          | with build-local program reuse                                                                                                                                                                 |
| ![Before program reuse](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-high-1000-gvrroot-memory.png) | ![After program reuse](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-high-1000-programcache-memory.png) |

Result:

- duration: `2m4s` -> `2m4s`
- peak working set: `4.62 GB` -> `1.75 GB`
- peak heap in use: `4.06 GB` -> `1.62 GB`
- peak CPU: `0.86` cores -> `0.65` cores

This is on the original RBAC/ConfigMap/SA benchmark shape, not the deployment-heavy debug workload.

References:

- [before summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-1000-high-gvrroot-20260308-1000/summary.json)
- [after summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-1000-high-programcache-20260308-1007/summary.json)

### 3. Reuse base env state, fast-path schema conversion, and reuse raw decl trees

Benchmark: original `high` shape, `5000` RGDs

There were multiple `5000`-RGD experiments. The pair below is:

- before: the `programcache` `5000` run
- after: the final `schemaenv` `5000` run

The `before` image is noisier because that run had a controller restart and a live resource-limit change mid-experiment. It is still the closest pre-fix `5000` screenshot pair we have.

| Before                                                                                                                                                                                                                   | After                                                                                                                                                                                                                |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `programcache` `5000` run                                                                                                                                                                                                | final `schemaenv` `5000` run                                                                                                                                                                                         |
| ![Before env/schema cleanup on the 5000-RGD run](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-high-5000-programcache-memory.png) | ![After env/schema cleanup on the 5000-RGD run](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/screenshots/various-optimizations/grafana-high-5000-schemaenv-memory.png) |

Result:

- `programcache` run: `20m33s`, `1` restart, `9.04 GB` peak working set, `8.44 GB` peak heap
- final `schemaenv` run: `11m29s`, `0` restarts, `8.55 GB` peak working set, `7.72 GB` peak heap

References:

- [before summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-programcache-20260308-1018/summary.json)
- [after summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/summary.json)

# WHICH `5000` RUN IS WHICH

| Label                    | Artifact                                                                                                                                                                                                                                                                                                       | Outcome                                                                                |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| pre-opt `5000` attempt   | [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L170) and [pprof-midrun](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-20260308-1506/pprof-midrun-20260308-1510) | OOMed before the first `1000`-RGD stage fully reconciled; no clean full summary bundle |
| `programcache` `5000`    | [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-programcache-20260308-1018/summary.json)                                                                                                                                 | finished, but with `1` restart and worse memory behavior                               |
| final `schemaenv` `5000` | [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-schemaenv-20260308-1046/summary.json)                                                                                                                                    | first clean `5000/5000 Active` run with `0` restarts                                   |
| `greenteagc` control     | [summary.json](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/results/rgd-scale-5000-high-greenteagc-20260308-1108/summary.json)                                                                                                                                   | slower and slightly worse on peak memory                                               |

# WHAT IS STILL NOT SOLVED

- retained CEL program / interpreter state still scales with active RGDs
- active RGDs still keep durable runtime state alive after activation
- one generated parent `GVR` still maps to one informer/watch pipeline
- the successful `5000`-RGD run still peaked around `8.55 GB` working set, `7.72 GB` heap, and about `60k` goroutines

That goroutine count should be read as "expected but expensive", not "mysterious leak". The hold-state goroutine profiles showed the controller was mostly parked in `runtime.gopark`, with the repeated stacks dominated by informer/watch code paths such as `Reflector.ListAndWatchWithContext`, `cache.(*controller).processLoop`, and `wait.(*Group).Start.func1`. In other words, the goroutine count is normal for the current one-watch-per-generated-`GVR` design, but the design itself still scales poorly.

# NEGATIVE CONTROL

`GOEXPERIMENT=greenteagc` was not a win on this workload:

- `11m29s` -> `12m18s`
- `8.55 GB` -> `8.59 GB` peak working set
- `7.72 GB` -> `8.04 GB` peak heap

Reference: [Findings.md](/Users/aminehilaly/source/github.com/kubernetes-sigs/kro-perf-testing/tools/stress-testing/Findings.md#L456)
