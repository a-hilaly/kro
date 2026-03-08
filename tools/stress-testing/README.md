# Stress Testing

`krostress` is a repo-local CLI for loading a live KRO controller and capturing the signals that matter during a run:

- synthetic RGD and instance creation at a fixed rate
- cleanup for generated resources
- Prometheus-backed CPU, memory, goroutine, and queue sampling
- pprof collection from the controller's debug service

## Build

From the repo root:

```bash
make stress-tool
```

Or from this directory:

```bash
make build
```

The binary is written to `bin/krostress`.

Investigation notes and confirmed performance observations live in `tools/stress-testing/Findings.md`.

The Grafana dashboard JSON used for this perf work lives in `tools/stress-testing/dashboards/kro-controller-metrics.json`.

## Assumptions

The default flags match the current perf cluster setup:

- controller namespace: `kro`
- controller metrics service: `kro-metrics`
- controller pprof service: `kro-pprof`
- Prometheus namespace: `monitoring`
- Prometheus service: `monitoring-kube-prometheus-prometheus`

Override any of those with flags if your environment differs.

## Common Flows

Create 100 medium RGDs at 20 per second:

```bash
./bin/krostress stress rgd create --total 100 --rate 20 --complexity medium
```

Create 100 deployment-heavy RGDs where each graph contains 50 `ConfigMap`s and 50 zero-replica `Deployment`s:

```bash
./bin/krostress stress rgd create --total 100 --rate 20 --complexity deployments
```

Create 5,000 instances for generated RGD `0`:

```bash
./bin/krostress stress instance create --rgd-index 0 --namespace default --total 5000 --rate 100
```

Capture controller CPU, memory, goroutines, and queue depth every 10 seconds for 15 minutes:

```bash
./bin/krostress observe sample --duration 15m --interval 10s
```

Collect a 30-second CPU profile:

```bash
./bin/krostress pprof collect cpu --seconds 30
```

Provision the checked-in Grafana dashboard into the cluster:

```bash
make -C tools/stress-testing dashboard-apply
```

Clean up generated instances and RGDs:

```bash
./bin/krostress stress instance cleanup --rgd-index 0 --namespace default
./bin/krostress stress rgd cleanup
```

## Notes

- Generated objects are labeled with `stress.kro.run/test=true` and `stress.kro.run/prefix=<prefix>`.
- The default prefix is `krostress`.
- `observe sample` writes JSON reports under `tools/stress-testing/results/`.
- The synthetic workloads use only built-in Kubernetes resources so they can run on a plain cluster without ACK CRDs.
