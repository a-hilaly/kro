package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kubernetes-sigs/kro/tools/stress-testing/internal/kube"
	stressutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/stress"
	"github.com/spf13/cobra"
)

func newStressCommand(root *RootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "stress",
		Short: "Create and clean up synthetic KRO workloads",
	}

	command.AddCommand(
		newStressRGDCommand(root),
		newStressInstanceCommand(root),
	)

	return command
}

func newStressRGDCommand(root *RootOptions) *cobra.Command {
	var (
		total       int
		rate        int
		complexity  string
		prefix      string
		waitActive  bool
		waitTimeout time.Duration
	)

	command := &cobra.Command{
		Use:   "rgd",
		Short: "Manage stress-test ResourceGraphDefinitions",
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create synthetic RGDs at a fixed rate",
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

			cfg, ok := stressutil.DefaultComplexities[strings.ToLower(complexity)]
			if !ok {
				return fmt.Errorf("unknown complexity %q", complexity)
			}

			ctx, stop := signalContext()
			defer stop()

			fmt.Printf("Creating %d RGDs at %d/sec (complexity: %s)\n", total, rate, complexity)
			result, err := stressutil.CreateResources(
				ctx,
				clients.Dynamic,
				stressutil.RGDGVR,
				"",
				total,
				rate,
				stressutil.RGDGenerator(prefix, cfg),
				func(progress stressutil.Progress) { printProgress(progress) },
			)

			fmt.Println()
			if err != nil && ctx.Err() == nil {
				return err
			}
			printResult(result)
			if result.Failed > 0 {
				return fmt.Errorf("%d of %d RGD creations failed", result.Failed, result.Total)
			}

			if !waitActive || total == 0 {
				return nil
			}

			fmt.Printf("Waiting up to %s for RGDs to become Active...\n", waitTimeout)
			waitCtx, cancel := context.WithTimeout(context.Background(), waitTimeout)
			defer cancel()

			for i := 0; i < total; i++ {
				if err := stressutil.WaitForRGDActive(waitCtx, clients.Dynamic, stressutil.RGDName(prefix, i), 2*time.Second); err != nil {
					return fmt.Errorf("wait for %s active: %w", stressutil.RGDName(prefix, i), err)
				}
			}

			fmt.Printf("All %d RGDs are Active\n", total)
			return nil
		},
	}

	createCmd.Flags().IntVar(&total, "total", 100, "number of RGDs to create")
	createCmd.Flags().IntVar(&rate, "rate", 10, "creation rate per second")
	createCmd.Flags().StringVar(&complexity, "complexity", "medium", "workload complexity: low, medium, high, or deployments")
	createCmd.Flags().StringVar(&prefix, "prefix", "krostress", "name prefix for generated resources")
	createCmd.Flags().BoolVar(&waitActive, "wait-active", false, "wait for each RGD to report Active after creation")
	createCmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 5*time.Minute, "timeout when waiting for RGDs to become Active")

	cleanupCmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete stress-test RGDs",
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

			selector := stressutil.LabelSelector(prefix)
			fmt.Printf("Deleting RGDs with selector %q...\n", selector)
			result, err := stressutil.DeleteResources(context.Background(), clients.Dynamic, stressutil.RGDGVR, "", selector, 50)
			if err != nil {
				return err
			}
			printDeleteResult("RGDs", result)
			return nil
		},
	}

	cleanupCmd.Flags().StringVar(&prefix, "prefix", "krostress", "resource prefix to target")
	command.AddCommand(createCmd, cleanupCmd)
	return command
}

func newStressInstanceCommand(root *RootOptions) *cobra.Command {
	var (
		total       int
		rate        int
		rgdIndex    int
		namespace   string
		prefix      string
		waitRGD     bool
		waitTimeout time.Duration
	)

	command := &cobra.Command{
		Use:   "instance",
		Short: "Manage stress-test RGD instances",
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create instances of a generated stress-test RGD",
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

			if waitRGD {
				fmt.Printf("Waiting up to %s for %s to become Active and publish its instance CRD...\n", waitTimeout, stressutil.RGDName(prefix, rgdIndex))
				waitCtx, cancel := context.WithTimeout(context.Background(), waitTimeout)
				defer cancel()

				if err := stressutil.WaitForRGDActive(waitCtx, clients.Dynamic, stressutil.RGDName(prefix, rgdIndex), 2*time.Second); err != nil {
					return fmt.Errorf("wait for RGD active: %w", err)
				}
				if err := stressutil.WaitForInstanceResource(waitCtx, clients.Discovery, prefix, rgdIndex, 2*time.Second); err != nil {
					return fmt.Errorf("wait for instance resource: %w", err)
				}
			}

			ctx, stop := signalContext()
			defer stop()

			fmt.Printf("Creating %d instances of %s at %d/sec in namespace %s\n", total, stressutil.InstanceKind(prefix, rgdIndex), rate, namespace)
			result, err := stressutil.CreateResources(
				ctx,
				clients.Dynamic,
				stressutil.InstanceGVR(prefix, rgdIndex),
				namespace,
				total,
				rate,
				stressutil.InstanceGenerator(prefix, rgdIndex, namespace),
				func(progress stressutil.Progress) { printProgress(progress) },
			)

			fmt.Println()
			if err != nil && ctx.Err() == nil {
				return err
			}
			printResult(result)
			if result.Failed > 0 {
				return fmt.Errorf("%d of %d instance creations failed", result.Failed, result.Total)
			}
			return nil
		},
	}

	createCmd.Flags().IntVar(&total, "total", 100, "number of instances to create")
	createCmd.Flags().IntVar(&rate, "rate", 10, "creation rate per second")
	createCmd.Flags().IntVar(&rgdIndex, "rgd-index", 0, "generated RGD index to target")
	createCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for generated instances")
	createCmd.Flags().StringVar(&prefix, "prefix", "krostress", "resource prefix to target")
	createCmd.Flags().BoolVar(&waitRGD, "wait-rgd", true, "wait for the generated RGD to become Active before creating instances")
	createCmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 5*time.Minute, "timeout when waiting for the RGD and its CRD")

	cleanupCmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete stress-test instances for a generated RGD",
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

			selector := stressutil.LabelSelector(prefix)
			fmt.Printf("Deleting instances with selector %q in namespace %s...\n", selector, namespace)
			result, err := stressutil.DeleteResources(context.Background(), clients.Dynamic, stressutil.InstanceGVR(prefix, rgdIndex), namespace, selector, 50)
			if err != nil {
				return err
			}
			printDeleteResult("instances", result)
			return nil
		},
	}

	cleanupCmd.Flags().IntVar(&rgdIndex, "rgd-index", 0, "generated RGD index to target")
	cleanupCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for generated instances")
	cleanupCmd.Flags().StringVar(&prefix, "prefix", "krostress", "resource prefix to target")

	command.AddCommand(createCmd, cleanupCmd)
	return command
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func printProgress(progress stressutil.Progress) {
	pct := 0.0
	if progress.Total > 0 {
		pct = float64(progress.Created+progress.Failed) / float64(progress.Total) * 100
	}
	fmt.Printf(
		"\r%.0f%% | %d created | %d failed | %.1f/sec",
		pct,
		progress.Created,
		progress.Failed,
		progress.Rate,
	)
}

func printResult(result *stressutil.Result) {
	fmt.Printf("Done in %s\n", result.Duration.Round(time.Millisecond))
	fmt.Printf("  Created:     %d\n", result.Created)
	fmt.Printf("  Failed:      %d\n", result.Failed)
	fmt.Printf("  Rate:        %.1f/sec\n", result.Rate)
	if result.AvgLatency > 0 {
		fmt.Printf("  Avg latency: %s\n", result.AvgLatency.Round(time.Millisecond))
	}
	if len(result.Errors) > 0 {
		fmt.Println("  Errors:")
		for _, err := range result.Errors {
			fmt.Printf("    - %s\n", err)
		}
	}
}

func printDeleteResult(kind string, result *stressutil.Result) {
	fmt.Printf("Deleted %d %s in %s\n", result.Created, kind, result.Duration.Round(time.Millisecond))
	if result.Failed > 0 {
		fmt.Printf("Failed to delete %d %s\n", result.Failed, kind)
	}
}
