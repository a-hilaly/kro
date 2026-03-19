package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubernetes-sigs/kro/tools/stress-testing/internal/kube"
	metricsutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/metrics"
	pprofutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/pprof"
	reportutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/report"
	stressutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/stress"
	"github.com/spf13/cobra"
)

func newRunCommand(root *RootOptions) *cobra.Command {
	var (
		total             int
		step              int
		rate              int
		complexity        string
		prefix            string
		outputDir         string
		observeInterval   time.Duration
		waitTimeout       time.Duration
		postStagePause    time.Duration
		promNamespace     string
		promService       string
		promPort          int
		pprofNamespace    string
		pprofService      string
		pprofPort         int
		controllerNS      string
		metricsService    string
		containerName     string
		podRegex          string
		cpuProfileSeconds int
	)

	command := &cobra.Command{
		Use:   "run",
		Short: "Run orchestrated scale tests and generate reports",
	}

	rgdScaleCmd := &cobra.Command{
		Use:   "rgd-scale",
		Short: "Create RGDs in stages, sample Prometheus, capture pprof, and write an HTML report",
		RunE: func(cmd *cobra.Command, args []string) error {
			if total <= 0 || step <= 0 || rate <= 0 {
				return fmt.Errorf("total, step, and rate must be > 0")
			}
			if total%step != 0 {
				return fmt.Errorf("total must be divisible by step for staged reporting")
			}

			complexity = strings.ToLower(strings.TrimSpace(complexity))
			cfg, ok := stressutil.DefaultComplexities[complexity]
			if !ok {
				return fmt.Errorf("unknown complexity %q", complexity)
			}

			if outputDir == "" {
				outputDir = filepath.Join(
					"tools",
					"stress-testing",
					"results",
					fmt.Sprintf("rgd-scale-%s", time.Now().UTC().Format("20060102-150405")),
				)
			}
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}

			ctx, stop := signalContext()
			defer stop()

			clients, err := kube.NewClients(kube.Options{
				Kubeconfig: root.Kubeconfig,
				Context:    root.Context,
				QPS:        root.QPS,
				Burst:      root.Burst,
			})
			if err != nil {
				return err
			}

			rgdList, err := clients.Dynamic.Resource(stressutil.RGDGVR).List(ctx, metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("list existing RGDs: %w", err)
			}
			existingRGDs := len(rgdList.Items)

			promForwarder, err := kube.PortForwardService(ctx, clients.Config, clients.Kubernetes, promNamespace, promService, promPort)
			if err != nil {
				return fmt.Errorf("port-forward prometheus: %w", err)
			}
			defer promForwarder.Close()

			pprofForwarder, err := kube.PortForwardService(ctx, clients.Config, clients.Kubernetes, pprofNamespace, pprofService, pprofPort)
			if err != nil {
				return fmt.Errorf("port-forward pprof: %w", err)
			}
			defer pprofForwarder.Close()

			observeOutput := filepath.Join(outputDir, "observation.json")
			markersOutput := filepath.Join(outputDir, "markers.json")
			summaryOutput := filepath.Join(outputDir, "summary.json")
			reportOutput := filepath.Join(outputDir, "report.html")
			profilesDir := filepath.Join(outputDir, "profiles")

			promClient := metricsutil.NewClient(promForwarder.URL())
			sampleCtx, sampleCancel := context.WithCancel(ctx)
			defer sampleCancel()

			type observeResult struct {
				report *metricsutil.Report
				err    error
			}

			observeCh := make(chan observeResult, 1)
			go func() {
				report, err := metricsutil.SampleController(sampleCtx, promClient, metricsutil.SampleOptions{
					ControllerNamespace: controllerNS,
					MetricsService:      metricsService,
					ContainerName:       containerName,
					PodRegex:            podRegex,
					Duration:            24 * time.Hour,
					Interval:            observeInterval,
				})
				observeCh <- observeResult{report: report, err: err}
			}()

			startedAt := time.Now().UTC()
			markers := []reportutil.StageMarker{
				{
					Label:       "0",
					Count:       0,
					Timestamp:   startedAt,
					Description: fmt.Sprintf("baseline with %d existing RGDs before the run", existingRGDs),
				},
			}

			var (
				cpuProfilePath  string
				heapProfilePath string
				cpuProfileErr   error
				heapProfileErr  error
			)

			cpuProfileDone := make(chan struct{})
			close(cpuProfileDone)
			cpuStarted := false

			for start := 0; start < total; start += step {
				stageIndex := start/step + 1
				stageEnd := start + step
				stageGenerator := stressutil.RGDGenerator(prefix, cfg)

				fmt.Printf("Stage %d/%d: creating RGDs %d-%d\n", stageIndex, total/step, start, stageEnd-1)

				if !cpuStarted && stageEnd == total {
					cpuStarted = true
					cpuProfileDone = make(chan struct{})
					go func() {
						defer close(cpuProfileDone)
						cpuProfilePath, cpuProfileErr = pprofutil.Collect(ctx, pprofForwarder.URL(), profilesDir, "cpu", cpuProfileSeconds)
					}()
				}

				result, err := stressutil.CreateResources(
					ctx,
					clients.Dynamic,
					stressutil.RGDGVR,
					"",
					step,
					rate,
					func(index int) *unstructured.Unstructured {
						return stageGenerator(start + index)
					},
					func(progress stressutil.Progress) { printProgress(progress) },
				)

				fmt.Println()
				if err != nil {
					return fmt.Errorf("create stage %d: %w", stageIndex, err)
				}
				printResult(result)
				if result.Failed > 0 {
					return fmt.Errorf("stage %d failed: %d of %d creations failed", stageIndex, result.Failed, result.Total)
				}

				waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
				fmt.Printf("Waiting for stage %d RGDs to become Active...\n", stageIndex)
				for i := start; i < stageEnd; i++ {
					if err := stressutil.WaitForRGDActive(waitCtx, clients.Dynamic, stressutil.RGDName(prefix, i), 2*time.Second); err != nil {
						cancel()
						return fmt.Errorf("wait for %s active: %w", stressutil.RGDName(prefix, i), err)
					}
					if completed := i - start + 1; completed%25 == 0 || completed == step {
						fmt.Printf("\rStage %d active progress: %d/%d", stageIndex, completed, step)
					}
				}
				cancel()
				fmt.Println()

				if err := sleepContext(ctx, postStagePause); err != nil {
					return err
				}

				markers = append(markers, reportutil.StageMarker{
					Label:       fmt.Sprintf("%d", stageEnd),
					Count:       stageEnd,
					Timestamp:   time.Now().UTC(),
					Description: fmt.Sprintf("%d RGDs created in this run (%d total in cluster)", stageEnd, existingRGDs+stageEnd),
				})
			}

			<-cpuProfileDone
			if cpuProfileErr != nil {
				fmt.Fprintf(os.Stderr, "cpu profile collection failed: %v\n", cpuProfileErr)
			}

			heapProfilePath, heapProfileErr = pprofutil.Collect(ctx, pprofForwarder.URL(), profilesDir, "heap", 0)
			if heapProfileErr != nil {
				fmt.Fprintf(os.Stderr, "heap profile collection failed: %v\n", heapProfileErr)
			}

			sampleCancel()
			observation := <-observeCh
			if observation.err != nil {
				return fmt.Errorf("collect Prometheus samples: %w", observation.err)
			}

			if err := metricsutil.WriteReport(observeOutput, observation.report); err != nil {
				return err
			}
			if err := writeJSON(markersOutput, markers); err != nil {
				return err
			}

			repoRoot, repoErr := findRepoRoot()
			symbolBinary := ""
			if repoErr == nil {
				symbolBinary, repoErr = buildControllerBinary(ctx, repoRoot, profilesDir)
			}

			cpuHotPaths := renderPprofTop(ctx, symbolBinary, cpuProfilePath)
			heapHotPaths := renderPprofTop(ctx, symbolBinary, heapProfilePath)
			if cpuProfileErr != nil {
				cpuHotPaths = fmt.Sprintf("CPU profile collection failed: %v", cpuProfileErr)
			}
			if heapProfileErr != nil {
				heapHotPaths = fmt.Sprintf("Heap profile collection failed: %v", heapProfileErr)
			}
			if repoErr != nil {
				note := fmt.Sprintf("Symbolization note: %v", repoErr)
				if cpuHotPaths != "" {
					cpuHotPaths = note + "\n\n" + cpuHotPaths
				}
				if heapHotPaths != "" {
					heapHotPaths = note + "\n\n" + heapHotPaths
				}
			}

			summary := reportutil.RunSummary{
				Title:           fmt.Sprintf("KRO RGD Scale Report - %s", startedAt.Format("2006-01-02 15:04:05 MST")),
				Lead:            "RGD scale report for KRO. This report combines Prometheus sampling, staged load markers, and pprof hot paths collected during the run.",
				UnitsLabel:      "RGDs Created",
				LevelLabel:      "RGD",
				StartedAt:       startedAt,
				EndedAt:         time.Now().UTC(),
				Duration:        time.Since(startedAt).Round(time.Second).String(),
				Complexity:      complexity,
				TotalUnits:      total,
				StepSize:        step,
				Rate:            rate,
				Prefix:          prefix,
				ExistingUnits:   existingRGDs,
				Observation:     observation.report,
				Markers:         markers,
				StageSnapshots:  reportutil.BuildStageSnapshots(observation.report, markers),
				CPUProfilePath:  cpuProfilePath,
				HeapProfilePath: heapProfilePath,
				CPUHotPaths:     cpuHotPaths,
				HeapHotPaths:    heapHotPaths,
			}

			if err := writeJSON(summaryOutput, summary); err != nil {
				return err
			}
			if err := reportutil.WriteHTML(reportOutput, summary); err != nil {
				return err
			}

			fmt.Printf("Wrote observation report to %s\n", observeOutput)
			fmt.Printf("Wrote stage markers to %s\n", markersOutput)
			fmt.Printf("Wrote summary to %s\n", summaryOutput)
			fmt.Printf("Wrote HTML report to %s\n", reportOutput)
			return nil
		},
	}

	rgdScaleCmd.Flags().IntVar(&total, "total", 5000, "total RGDs to create")
	rgdScaleCmd.Flags().IntVar(&step, "step", 1000, "RGD increment per stage")
	rgdScaleCmd.Flags().IntVar(&rate, "rate", 25, "creation rate per second")
	rgdScaleCmd.Flags().StringVar(&complexity, "complexity", "high", "RGD complexity: low, medium, high, deployments, or real")
	rgdScaleCmd.Flags().StringVar(&prefix, "prefix", "rgdscale", "prefix for generated RGD names")
	rgdScaleCmd.Flags().StringVar(&outputDir, "output-dir", "", "directory for report artifacts")
	rgdScaleCmd.Flags().DurationVar(&observeInterval, "observe-interval", 10*time.Second, "Prometheus sampling interval")
	rgdScaleCmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 30*time.Minute, "timeout per stage while waiting for RGDs to become Active")
	rgdScaleCmd.Flags().DurationVar(&postStagePause, "post-stage-pause", 20*time.Second, "extra pause after each stage before recording a marker")
	rgdScaleCmd.Flags().StringVar(&promNamespace, "prom-namespace", "monitoring", "Prometheus namespace")
	rgdScaleCmd.Flags().StringVar(&promService, "prom-service", "monitoring-kube-prometheus-prometheus", "Prometheus service")
	rgdScaleCmd.Flags().IntVar(&promPort, "prom-port", 9090, "Prometheus port")
	rgdScaleCmd.Flags().StringVar(&pprofNamespace, "pprof-namespace", "kro", "pprof namespace")
	rgdScaleCmd.Flags().StringVar(&pprofService, "pprof-service", "kro-pprof", "pprof service")
	rgdScaleCmd.Flags().IntVar(&pprofPort, "pprof-port", 6060, "pprof service port")
	rgdScaleCmd.Flags().StringVar(&controllerNS, "controller-namespace", "kro", "namespace where the controller runs")
	rgdScaleCmd.Flags().StringVar(&metricsService, "metrics-service", "kro-metrics", "controller metrics service name")
	rgdScaleCmd.Flags().StringVar(&containerName, "container", "kro", "controller container name for kubelet CPU and memory series")
	rgdScaleCmd.Flags().StringVar(&podRegex, "pod-regex", "kro-.*", "regex for matching controller pod names in Prometheus")
	rgdScaleCmd.Flags().IntVar(&cpuProfileSeconds, "cpu-profile-seconds", 60, "CPU pprof duration in seconds")

	command.AddCommand(rgdScaleCmd)
	return command
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func writeJSON(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func findRepoRoot() (string, error) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe), filepath.Dir(filepath.Dir(exe)))
	}

	seen := map[string]struct{}{}
	for _, start := range candidates {
		dir := start
		for {
			if _, ok := seen[dir]; ok {
				break
			}
			seen[dir] = struct{}{}

			if fileExists(filepath.Join(dir, "go.mod")) && fileExists(filepath.Join(dir, "cmd", "controller", "main.go")) {
				return dir, nil
			}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return "", fmt.Errorf("unable to locate repo root for controller symbolization")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func buildControllerBinary(ctx context.Context, repoRoot, outputDir string) (string, error) {
	path := filepath.Join(outputDir, "controller-symbols")
	cmd := exec.CommandContext(ctx, "go", "build", "-tags=pprof", "-o", path, "./cmd/controller/main.go")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build controller binary: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return path, nil
}

func renderPprofTop(ctx context.Context, binaryPath, profilePath string) string {
	if profilePath == "" {
		return "profile not collected"
	}

	args := []string{"tool", "pprof", "-top", "-nodecount=25"}
	if binaryPath != "" {
		args = append(args, binaryPath)
	}
	args = append(args, profilePath)

	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("go %s failed: %v\n\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}
