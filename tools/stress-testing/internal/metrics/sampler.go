package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"
)

type SampleOptions struct {
	ControllerNamespace string
	MetricsService      string
	ContainerName       string
	PodRegex            string
	Duration            time.Duration
	Interval            time.Duration
}

type Sample struct {
	Timestamp time.Time          `json:"timestamp"`
	Values    map[string]float64 `json:"values"`
}

type MetricSummary struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Avg  float64 `json:"avg"`
	Last float64 `json:"last"`
}

type Report struct {
	StartedAt time.Time                `json:"startedAt"`
	EndedAt   time.Time                `json:"endedAt"`
	Interval  time.Duration            `json:"interval"`
	Queries   map[string]string        `json:"queries"`
	Samples   []Sample                 `json:"samples"`
	Summary   map[string]MetricSummary `json:"summary"`
}

func DefaultControllerQueries(opts SampleOptions) map[string]string {
	podRegex := opts.PodRegex
	if podRegex == "" {
		podRegex = ".*"
	}

	return map[string]string{
		"cpu_cores": fmt.Sprintf(
			`sum(rate(container_cpu_usage_seconds_total{namespace=%q,container=%q,pod=~%q,image!=""}[2m]))`,
			opts.ControllerNamespace,
			opts.ContainerName,
			podRegex,
		),
		"memory_working_set_bytes": fmt.Sprintf(
			`sum(container_memory_working_set_bytes{namespace=%q,container=%q,pod=~%q,image!=""})`,
			opts.ControllerNamespace,
			opts.ContainerName,
			podRegex,
		),
		"restarts_total": fmt.Sprintf(
			`sum(kube_pod_container_status_restarts_total{namespace=%q,container=%q,pod=~%q})`,
			opts.ControllerNamespace,
			opts.ContainerName,
			podRegex,
		),
		"go_goroutines": fmt.Sprintf(
			`sum(go_goroutines{namespace=%q,service=%q})`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"heap_inuse_bytes": fmt.Sprintf(
			`sum(go_memstats_heap_inuse_bytes{namespace=%q,service=%q})`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"resident_memory_bytes": fmt.Sprintf(
			`sum(process_resident_memory_bytes{namespace=%q,service=%q})`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"active_workers": fmt.Sprintf(
			`sum(controller_runtime_active_workers{namespace=%q,service=%q})`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"workqueue_depth": fmt.Sprintf(
			`sum(workqueue_depth{namespace=%q,service=%q})`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"dynamic_controller_queue_length": fmt.Sprintf(
			`sum(dynamic_controller_queue_length{namespace=%q,service=%q})`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"reconcile_rate_per_second": fmt.Sprintf(
			`sum(rate(controller_runtime_reconcile_total{namespace=%q,service=%q}[2m]))`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
		"reconcile_errors_rate_per_second": fmt.Sprintf(
			`sum(rate(controller_runtime_reconcile_errors_total{namespace=%q,service=%q}[2m]))`,
			opts.ControllerNamespace,
			opts.MetricsService,
		),
	}
}

func SampleController(ctx context.Context, client *Client, opts SampleOptions) (*Report, error) {
	if opts.Interval <= 0 {
		opts.Interval = 15 * time.Second
	}

	queries := DefaultControllerQueries(opts)
	report := &Report{
		StartedAt: time.Now().UTC(),
		Interval:  opts.Interval,
		Queries:   maps.Clone(queries),
	}

	sampleOnce := func(now time.Time) error {
		sample := Sample{
			Timestamp: now.UTC(),
			Values:    make(map[string]float64, len(queries)),
		}

		for name, query := range queries {
			value, err := client.QueryNumber(ctx, query, now)
			if err != nil {
				return fmt.Errorf("query %s: %w", name, err)
			}
			sample.Values[name] = value
		}

		report.Samples = append(report.Samples, sample)
		return nil
	}

	if err := sampleOnce(time.Now()); err != nil {
		return nil, err
	}

	if opts.Duration > 0 {
		deadline := time.Now().Add(opts.Duration)
		ticker := time.NewTicker(opts.Interval)
		defer ticker.Stop()

		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				report.EndedAt = time.Now().UTC()
				report.Summary = summarize(report.Samples)
				return report, nil
			case tick := <-ticker.C:
				if err := sampleOnce(tick); err != nil {
					return nil, err
				}
			}
		}
	}

	report.EndedAt = time.Now().UTC()
	report.Summary = summarize(report.Samples)
	return report, nil
}

func WriteReport(outputPath string, report *Report) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(outputPath, payload, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	return nil
}

func summarize(samples []Sample) map[string]MetricSummary {
	summary := map[string]MetricSummary{}
	if len(samples) == 0 {
		return summary
	}

	for _, sample := range samples {
		for name, value := range sample.Values {
			stat, ok := summary[name]
			if !ok {
				summary[name] = MetricSummary{
					Min:  value,
					Max:  value,
					Avg:  value,
					Last: value,
				}
				continue
			}

			if value < stat.Min {
				stat.Min = value
			}
			if value > stat.Max {
				stat.Max = value
			}
			stat.Avg += value
			stat.Last = value
			summary[name] = stat
		}
	}

	for name, stat := range summary {
		stat.Avg = stat.Avg / float64(len(samples))
		summary[name] = stat
	}

	return summary
}
