package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kubernetes-sigs/kro/tools/stress-testing/internal/kube"
	pprofutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/pprof"
	"github.com/spf13/cobra"
)

func newPprofCommand(root *RootOptions) *cobra.Command {
	var (
		namespace  string
		service    string
		port       int
		outputDir  string
		cpuSeconds int
	)

	command := &cobra.Command{
		Use:   "pprof",
		Short: "Collect pprof profiles from a live KRO controller",
	}

	collectCmd := &cobra.Command{
		Use:   "collect [type]",
		Short: "Collect a pprof profile: cpu, heap, goroutine, allocs, block, mutex, or all",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileType := "heap"
			if len(args) == 1 {
				profileType = args[0]
			}
			if outputDir == "" {
				outputDir = filepath.Join("tools", "stress-testing", "results", "profiles")
			}

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
			forwarder, err := kube.PortForwardService(ctx, clients.Config, clients.Kubernetes, namespace, service, port)
			if err != nil {
				return fmt.Errorf("port-forward pprof service: %w", err)
			}
			defer forwarder.Close()

			if profileType == "all" {
				for _, current := range []string{"heap", "goroutine", "allocs", "block", "mutex", "cpu"} {
					path, err := pprofutil.Collect(ctx, forwarder.URL(), outputDir, current, cpuSeconds)
					if err != nil {
						return err
					}
					fmt.Printf("Saved %s\n", path)
				}
				return nil
			}

			path, err := pprofutil.Collect(ctx, forwarder.URL(), outputDir, profileType, cpuSeconds)
			if err != nil {
				return err
			}

			fmt.Printf("Saved %s\n", path)
			return nil
		},
	}

	collectCmd.Flags().StringVarP(&namespace, "namespace", "n", "kro", "namespace where the pprof service runs")
	collectCmd.Flags().StringVar(&service, "service", "kro-pprof", "pprof service name")
	collectCmd.Flags().IntVar(&port, "port", 6060, "pprof service port")
	collectCmd.Flags().StringVar(&outputDir, "output", "", "directory to write profiles into")
	collectCmd.Flags().IntVar(&cpuSeconds, "seconds", 30, "duration for CPU profiles")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List collected pprof profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := outputDir
			if dir == "" {
				dir = filepath.Join("tools", "stress-testing", "results", "profiles")
			}

			profiles, err := pprofutil.ListProfiles(dir)
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				fmt.Println("No profiles found")
				return nil
			}

			fmt.Printf("Profiles in %s:\n", dir)
			for _, profile := range profiles {
				fmt.Printf("  %s\n", profile)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&outputDir, "output", filepath.Join("tools", "stress-testing", "results", "profiles"), "directory to inspect")

	command.AddCommand(collectCmd, listCmd)
	return command
}
