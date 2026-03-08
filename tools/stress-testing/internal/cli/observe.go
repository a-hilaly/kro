package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/kubernetes-sigs/kro/tools/stress-testing/internal/kube"
	metricsutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/metrics"
	"github.com/spf13/cobra"
)

func newObserveCommand(root *RootOptions) *cobra.Command {
	var (
		controllerNamespace string
		metricsService      string
		containerName       string
		podRegex            string
		promNamespace       string
		promService         string
		promPort            int
		duration            time.Duration
		interval            time.Duration
		output              string
	)

	command := &cobra.Command{
		Use:   "observe",
		Short: "Capture controller CPU, memory, queue, and reconcile metrics from Prometheus",
	}

	sampleCmd := &cobra.Command{
		Use:   "sample",
		Short: "Sample controller resource and Prometheus metrics over time",
		RunE: func(cmd *cobra.Command, args []string) error {
			clients, err := kube.NewClients(kube.Options{
				Kubeconfig: root.Kubeconfig,
				Context:    root.Context,
				QPS:        root.QPS,
				Burst:      root.Burst,
			})
			if err != nil {
				return err
			}

			ctx := context.Background()
			forwarder, err := kube.PortForwardService(ctx, clients.Config, clients.Kubernetes, promNamespace, promService, promPort)
			if err != nil {
				return fmt.Errorf("port-forward prometheus: %w", err)
			}
			defer forwarder.Close()

			promClient := metricsutil.NewClient(forwarder.URL())
			report, err := metricsutil.SampleController(ctx, promClient, metricsutil.SampleOptions{
				ControllerNamespace: controllerNamespace,
				MetricsService:      metricsService,
				ContainerName:       containerName,
				PodRegex:            podRegex,
				Duration:            duration,
				Interval:            interval,
			})
			if err != nil {
				return err
			}

			if output == "" {
				output = filepath.Join(
					"tools",
					"stress-testing",
					"results",
					fmt.Sprintf("observation-%s.json", time.Now().UTC().Format("20060102-150405")),
				)
			}

			if err := metricsutil.WriteReport(output, report); err != nil {
				return err
			}

			fmt.Printf("Wrote %d samples to %s\n", len(report.Samples), output)
			printMetricSummary("CPU cores", report.Summary["cpu_cores"], "")
			printMetricSummary("Memory working set", report.Summary["memory_working_set_bytes"], "bytes")
			printMetricSummary("Resident memory", report.Summary["resident_memory_bytes"], "bytes")
			printMetricSummary("Heap in use", report.Summary["heap_inuse_bytes"], "bytes")
			printMetricSummary("Goroutines", report.Summary["go_goroutines"], "")
			printMetricSummary("Active workers", report.Summary["active_workers"], "")
			printMetricSummary("Workqueue depth", report.Summary["workqueue_depth"], "")
			printMetricSummary("Dynamic queue length", report.Summary["dynamic_controller_queue_length"], "")
			return nil
		},
	}

	sampleCmd.Flags().StringVar(&controllerNamespace, "controller-namespace", "kro", "namespace where the controller runs")
	sampleCmd.Flags().StringVar(&metricsService, "metrics-service", "kro-metrics", "controller metrics service name")
	sampleCmd.Flags().StringVar(&containerName, "container", "kro", "controller container name for kubelet CPU and memory series")
	sampleCmd.Flags().StringVar(&podRegex, "pod-regex", "kro-.*", "regex for matching controller pod names in Prometheus")
	sampleCmd.Flags().StringVar(&promNamespace, "prom-namespace", "monitoring", "namespace where Prometheus runs")
	sampleCmd.Flags().StringVar(&promService, "prom-service", "monitoring-kube-prometheus-prometheus", "Prometheus service name")
	sampleCmd.Flags().IntVar(&promPort, "prom-port", 9090, "Prometheus service port")
	sampleCmd.Flags().DurationVar(&duration, "duration", 5*time.Minute, "total sampling duration; set to 0 for a one-shot snapshot")
	sampleCmd.Flags().DurationVar(&interval, "interval", 15*time.Second, "sampling interval")
	sampleCmd.Flags().StringVar(&output, "output", "", "write the report to this JSON path")

	command.AddCommand(sampleCmd)
	return command
}

func printMetricSummary(name string, summary metricsutil.MetricSummary, unit string) {
	if unit == "bytes" {
		fmt.Printf(
			"%s: avg=%s max=%s last=%s\n",
			name,
			formatBytes(summary.Avg),
			formatBytes(summary.Max),
			formatBytes(summary.Last),
		)
		return
	}

	fmt.Printf("%s: avg=%.2f max=%.2f last=%.2f\n", name, summary.Avg, summary.Max, summary.Last)
}

func formatBytes(value float64) string {
	const (
		ki = 1024
		mi = ki * 1024
		gi = mi * 1024
	)

	switch {
	case value >= gi:
		return fmt.Sprintf("%.2f GiB", value/gi)
	case value >= mi:
		return fmt.Sprintf("%.2f MiB", value/mi)
	case value >= ki:
		return fmt.Sprintf("%.2f KiB", value/ki)
	default:
		return fmt.Sprintf("%.0f B", value)
	}
}
