package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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
		total            int
		rate             int
		deleteWorkers    int
		complexity       string
		planComplexity   string
		circusDropMin    int
		circusDropMax    int
		circusSeed       int64
		prefix           string
		waitActive       bool
		waitTimeout      time.Duration
		hierarchy        string
		parentInstances  int
		targetTotal      int
		includeInstances bool
		checkpointStep   int
		outputDir        string
		writeRender      bool
		crdURLs          []string
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
			if cfg.Preset == "circus" {
				cfg.DropMin = circusDropMin
				cfg.DropMax = circusDropMax
				cfg.Seed = circusSeed
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
	createCmd.Flags().StringVar(&complexity, "complexity", "medium", "workload complexity: low, medium, high, deployments, circus, feature-mix, or real")
	createCmd.Flags().IntVar(&circusDropMin, "circus-drop-min", 5, "minimum number of optional nodes to drop when complexity=circus")
	createCmd.Flags().IntVar(&circusDropMax, "circus-drop-max", 50, "maximum number of optional nodes to drop when complexity=circus")
	createCmd.Flags().Int64Var(&circusSeed, "circus-seed", 1, "deterministic seed used when complexity=circus")
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
			result, err := stressutil.DeleteResources(context.Background(), clients.Dynamic, stressutil.RGDGVR, "", selector, deleteWorkers)
			if err != nil {
				return err
			}
			printDeleteResult("RGDs", result)
			return nil
		},
	}

	cleanupCmd.Flags().StringVar(&prefix, "prefix", "krostress", "resource prefix to target")
	cleanupCmd.Flags().IntVar(&deleteWorkers, "delete-workers", 50, "maximum parallel delete requests for matched RGDs")

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Apply a rendered hierarchy and wait for its RGDs to become Active",
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

			rendered, err := stressutil.RenderHierarchy(hierarchy)
			if err != nil {
				return err
			}
			if len(rendered) == 0 {
				return fmt.Errorf("hierarchy %q rendered no RGDs", hierarchy)
			}

			ctx, stop := signalContext()
			defer stop()

			missingKinds, err := stressutil.MissingRenderedKinds(clients.Discovery, rendered)
			if err != nil {
				return err
			}
			if len(missingKinds) > 0 {
				fmt.Println("Hierarchy prerequisites are missing from the cluster:")
				for _, missing := range missingKinds {
					fmt.Printf("  - %s %s\n", missing.APIVersion, missing.Kind)
				}
				return fmt.Errorf("cannot apply hierarchy %q until the required kinds are installed", hierarchy)
			}

			if writeRender {
				targetDir := outputDir
				if strings.TrimSpace(targetDir) == "" {
					targetDir = filepath.Join(
						"tools",
						"stress-testing",
						"results",
						fmt.Sprintf("%s-setup-%s", hierarchy, time.Now().Format("20060102-150405")),
					)
				}

				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					return fmt.Errorf("create output dir: %w", err)
				}
				summaryPath := filepath.Join(targetDir, "00-summary.txt")
				if err := os.WriteFile(summaryPath, []byte(stressutil.HierarchySummary(rendered)), 0o644); err != nil {
					return fmt.Errorf("write summary: %w", err)
				}
				for _, manifest := range rendered {
					path := filepath.Join(targetDir, manifest.Filename)
					if err := os.WriteFile(path, manifest.YAML, 0o644); err != nil {
						return fmt.Errorf("write %s: %w", manifest.Filename, err)
					}
				}
				fmt.Printf("Wrote rendered hierarchy to %s\n", targetDir)
			}

			applyObjects := make([]*unstructured.Unstructured, 0, len(rendered))
			for i := len(rendered) - 1; i >= 0; i-- {
				applyObjects = append(applyObjects, rendered[i].Object)
			}

			fmt.Printf("Applying %d RGDs for hierarchy %q in dependency order...\n", len(applyObjects), hierarchy)
			result, err := stressutil.ApplyResources(
				ctx,
				clients.Dynamic,
				stressutil.RGDGVR,
				"",
				applyObjects,
				"krostress",
			)
			if err != nil {
				return err
			}
			printResult(result)
			if result.Failed > 0 {
				return fmt.Errorf("%d of %d RGD applies failed", result.Failed, result.Total)
			}

			fmt.Printf("Waiting up to %s for %d RGDs to become Active...\n", waitTimeout, len(rendered))
			waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
			defer cancel()

			totalWait := len(rendered)
			completed := 0
			for i := len(rendered) - 1; i >= 0; i-- {
				if err := stressutil.WaitForRGDActive(waitCtx, clients.Dynamic, rendered[i].Definition.ResourceGraphName, 2*time.Second); err != nil {
					return fmt.Errorf("wait for %s active: %w", rendered[i].Definition.ResourceGraphName, err)
				}
				completed++
				fmt.Printf("\r%d/%d active", completed, totalWait)
			}
			fmt.Println()

			root := rendered[0].Definition
			fmt.Printf("Top-level RGD:   %s\n", root.ResourceGraphName)
			fmt.Printf("Top-level kind:  %s\n", root.SchemaKind)
			fmt.Printf("Top-level api:   kro.run/v1alpha1\n")
			return nil
		},
	}
	setupCmd.Flags().StringVar(&hierarchy, "hierarchy", "graph-heavy", "hierarchy preset to apply")
	setupCmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 20*time.Minute, "timeout when waiting for hierarchy RGDs to become Active")
	setupCmd.Flags().BoolVar(&writeRender, "write-render", true, "write the rendered hierarchy manifests to disk before apply")
	setupCmd.Flags().StringVar(&outputDir, "output-dir", "", "directory to write rendered manifests into when --write-render is enabled")

	setupCRDsCmd := &cobra.Command{
		Use:   "setup-crds",
		Short: "Fetch CRD manifests from URLs and apply them to the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(crdURLs) == 0 {
				return fmt.Errorf("at least one --url is required")
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

			ctx, stop := signalContext()
			defer stop()

			crds, err := stressutil.FetchCRDsFromURLs(ctx, crdURLs)
			if err != nil {
				return err
			}

			fmt.Printf("Applying %d CRDs fetched from %d URL(s)...\n", len(crds), len(crdURLs))
			result, err := stressutil.ApplyResources(
				ctx,
				clients.Dynamic,
				stressutil.CRDGVR,
				"",
				crds,
				"krostress-crd-setup",
			)
			if err != nil {
				return err
			}
			printResult(result)
			if result.Failed > 0 {
				return fmt.Errorf("%d of %d CRD applies failed", result.Failed, result.Total)
			}

			fmt.Println("Applied CRDs:")
			for _, crd := range crds {
				fmt.Printf("  - %s\n", crd.GetName())
			}
			return nil
		},
	}
	setupCRDsCmd.Flags().StringArrayVar(&crdURLs, "url", nil, "URL to a YAML manifest containing one or more CustomResourceDefinitions")

	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Project hierarchy or synthetic RGD counts without creating anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(planComplexity) != "" {
				cfg, ok := stressutil.DefaultComplexities[strings.ToLower(strings.TrimSpace(planComplexity))]
				if !ok {
					return fmt.Errorf("unknown complexity %q", planComplexity)
				}
				if cfg.Preset == "circus" {
					cfg.DropMin = circusDropMin
					cfg.DropMax = circusDropMax
					cfg.Seed = circusSeed
				}
				if total < 0 {
					return fmt.Errorf("total must be >= 0")
				}
				if checkpointStep < 0 {
					return fmt.Errorf("checkpoint-step must be >= 0")
				}

				if checkpointStep > 0 {
					checkpoints, err := stressutil.PlanSyntheticRGDCheckpoints(prefix, cfg, total, checkpointStep)
					if err != nil {
						return err
					}
					fmt.Printf("Synthetic complexity: %s\n", strings.ToLower(strings.TrimSpace(planComplexity)))
					fmt.Printf("Prefix: %s\n", prefix)
					fmt.Printf("RGDs planned: %d\n", total)
					fmt.Printf("Checkpoint step: %d\n", checkpointStep)
					fmt.Println("Checkpoint averages:")
					for _, checkpoint := range checkpoints {
						fmt.Printf("- %d RGDs: avg resources %.2f, avg status leaves %.2f, unique resource shapes %d, unique status shapes %d\n",
							checkpoint.TotalRGDs,
							checkpoint.AverageResources,
							checkpoint.AverageStatusLeaves,
							checkpoint.UniqueResourceShapes,
							checkpoint.UniqueStatusShapes,
						)
					}
					return nil
				}

				plan, err := stressutil.PlanSyntheticRGDs(prefix, cfg, total)
				if err != nil {
					return err
				}

				fmt.Printf("Synthetic complexity: %s\n", strings.ToLower(strings.TrimSpace(planComplexity)))
				fmt.Printf("Prefix: %s\n", prefix)
				fmt.Printf("RGDs planned: %d\n", plan.TotalRGDs)
				fmt.Println("Per RGD resources:")
				fmt.Printf("- total resources across RGDs: %d\n", plan.TotalResources)
				fmt.Printf("- average resources per RGD: %.2f\n", plan.AverageResources)
				fmt.Printf("- min resources: %d (index %d)\n", plan.MinResources, plan.MinResourcesIndex)
				fmt.Printf("- max resources: %d (index %d)\n", plan.MaxResources, plan.MaxResourcesIndex)
				fmt.Println("Status schema leaves:")
				fmt.Printf("- total status leaves across RGDs: %d\n", plan.TotalStatusLeaves)
				fmt.Printf("- average status leaves per RGD: %.2f\n", plan.AverageStatusLeaves)
				fmt.Printf("- min status leaves: %d (index %d)\n", plan.MinStatusLeaves, plan.MinStatusLeavesIndex)
				fmt.Printf("- max status leaves: %d (index %d)\n", plan.MaxStatusLeaves, plan.MaxStatusLeavesIndex)
				fmt.Println("Shape variation:")
				fmt.Printf("- unique resource shapes: %d\n", plan.UniqueResourceShapes)
				fmt.Printf("- unique status shapes: %d\n", plan.UniqueStatusShapes)
				return nil
			}

			profile, ok := stressutil.HierarchyByName(strings.ToLower(strings.TrimSpace(hierarchy)))
			if !ok {
				return fmt.Errorf("unknown hierarchy %q", hierarchy)
			}
			if parentInstances < 0 {
				return fmt.Errorf("parent-instances must be >= 0")
			}
			if targetTotal < 0 {
				return fmt.Errorf("target-total must be >= 0")
			}

			fmt.Printf("Hierarchy: %s\n", profile.Name)
			fmt.Printf("RGD definitions: %d\n", profile.DefinitionCount())
			fmt.Println("Per parent instance:")
			fmt.Printf("- leaf resources: %d\n", profile.LeafResourcesPerParentInstance())
			fmt.Printf("- child instance CRs: %d\n", profile.ChildInstancesPerParentInstance())
			fmt.Printf("- total instance CRs: %d\n", profile.InstanceCRsPerParentInstance())
			fmt.Printf("- total objects including instances: %d\n", profile.ObjectsPerParentInstance(true))

			if parentInstances > 0 {
				projection := profile.Project(parentInstances)
				fmt.Printf("\nProjection for %d parent instances:\n", parentInstances)
				fmt.Printf("- leaf resources: %d\n", projection.LeafResources)
				fmt.Printf("- child instance CRs: %d\n", projection.ChildInstanceCRs)
				fmt.Printf("- total instance CRs: %d\n", projection.TotalInstanceCRs)
				fmt.Printf("- total objects including instances: %d\n", projection.TotalObjects)
			}

			if targetTotal > 0 {
				projection, err := profile.ProjectForTarget(targetTotal, includeInstances)
				if err != nil {
					return err
				}

				modeLabel := "leaf resources only"
				if includeInstances {
					modeLabel = "total objects including instances"
				}
				fmt.Printf("\nProjection for target total %d (%s):\n", targetTotal, modeLabel)
				fmt.Printf("- parent instances: %d\n", projection.ParentInstances)
				fmt.Printf("- leaf resources: %d\n", projection.LeafResources)
				fmt.Printf("- child instance CRs: %d\n", projection.ChildInstanceCRs)
				fmt.Printf("- total instance CRs: %d\n", projection.TotalInstanceCRs)
				fmt.Printf("- total objects including instances: %d\n", projection.TotalObjects)
			}

			if parentInstances == 0 && targetTotal == 0 {
				fmt.Println("\nTip: use --parent-instances or --target-total for a concrete projection.")
			}
			return nil
		},
	}
	planCmd.Flags().StringVar(&hierarchy, "hierarchy", "graph-heavy", "hierarchy preset to project")
	planCmd.Flags().IntVar(&parentInstances, "parent-instances", 0, "number of top-level parent instances to project")
	planCmd.Flags().IntVar(&targetTotal, "target-total", 0, "desired total count to backsolve into parent instances")
	planCmd.Flags().BoolVar(&includeInstances, "include-instances", false, "treat --target-total as total objects including instance CRs")
	planCmd.Flags().StringVar(&planComplexity, "complexity", "", "synthetic RGD complexity to project instead of hierarchy: low, medium, high, deployments, circus, feature-mix, or real")
	planCmd.Flags().IntVar(&total, "total", 0, "number of synthetic RGDs to project when --complexity is set")
	planCmd.Flags().StringVar(&prefix, "prefix", "krostress", "name prefix for synthetic planning")
	planCmd.Flags().IntVar(&checkpointStep, "checkpoint-step", 0, "emit cumulative synthetic projections every N RGDs from a single pass")
	planCmd.Flags().IntVar(&circusDropMin, "circus-drop-min", 5, "minimum number of optional nodes to drop when --complexity=circus")
	planCmd.Flags().IntVar(&circusDropMax, "circus-drop-max", 50, "maximum number of optional nodes to drop when --complexity=circus")
	planCmd.Flags().Int64Var(&circusSeed, "circus-seed", 1, "deterministic seed used when --complexity=circus")

	renderCmd := &cobra.Command{
		Use:   "render",
		Short: "Render hierarchy RGDs to disk without applying them",
		RunE: func(cmd *cobra.Command, args []string) error {
			rendered, err := stressutil.RenderHierarchy(hierarchy)
			if err != nil {
				return err
			}

			targetDir := outputDir
			if strings.TrimSpace(targetDir) == "" {
				targetDir = filepath.Join(
					"tools",
					"stress-testing",
					"results",
					fmt.Sprintf("%s-render-%s", hierarchy, time.Now().Format("20060102-150405")),
				)
			}

			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}

			summaryPath := filepath.Join(targetDir, "00-summary.txt")
			if err := os.WriteFile(summaryPath, []byte(stressutil.HierarchySummary(rendered)), 0o644); err != nil {
				return fmt.Errorf("write summary: %w", err)
			}

			fmt.Printf("Rendered hierarchy %q to %s\n", hierarchy, targetDir)
			fmt.Printf("Summary: %s\n", summaryPath)
			for _, manifest := range rendered {
				path := filepath.Join(targetDir, manifest.Filename)
				if err := os.WriteFile(path, manifest.YAML, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", manifest.Filename, err)
				}
				fmt.Printf("- %s\n", path)
			}

			return nil
		},
	}
	renderCmd.Flags().StringVar(&hierarchy, "hierarchy", "graph-heavy", "hierarchy preset to render")
	renderCmd.Flags().StringVar(&outputDir, "output-dir", "", "directory to write rendered manifests into")

	command.AddCommand(createCmd, cleanupCmd, setupCmd, setupCRDsCmd, planCmd, renderCmd)
	return command
}

func newStressInstanceCommand(root *RootOptions) *cobra.Command {
	var (
		total                int
		rate                 int
		rgdIndex             int
		startIndex           int
		instanceName         string
		deleteWorkers        int
		ownedCleanupWorkers  int
		hierarchy            string
		namespace            string
		prefix               string
		waitRGD              bool
		waitTimeout          time.Duration
		cleanupOwnedChildren bool
		sweepOnly            bool
	)

	command := &cobra.Command{
		Use:   "instance",
		Short: "Manage stress-test RGD instances",
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create instances of a generated stress-test RGD or hierarchy root",
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

			targetGVR := stressutil.InstanceGVR(prefix, rgdIndex)
			targetName := stressutil.InstanceKind(prefix, rgdIndex)
			baseGenerator := stressutil.InstanceGenerator(prefix, rgdIndex, namespace)
			waitRGDName := stressutil.RGDName(prefix, rgdIndex)

			if hierarchy != "" {
				hierarchyGVR, rootDefinition, generator, err := stressutil.HierarchyInstanceGenerator(hierarchy, prefix, namespace)
				if err != nil {
					return fmt.Errorf("resolve hierarchy %q: %w", hierarchy, err)
				}
				targetGVR = hierarchyGVR
				targetName = rootDefinition.SchemaKind
				baseGenerator = generator
				waitRGDName = rootDefinition.ResourceGraphName
			}

			if waitRGD {
				fmt.Printf("Waiting up to %s for %s to become Active and publish its instance CRD...\n", waitTimeout, waitRGDName)
				waitCtx, cancel := context.WithTimeout(context.Background(), waitTimeout)
				defer cancel()

				if err := stressutil.WaitForRGDActive(waitCtx, clients.Dynamic, waitRGDName, 2*time.Second); err != nil {
					return fmt.Errorf("wait for RGD active: %w", err)
				}
				if err := stressutil.WaitForInstanceResourceByName(waitCtx, clients.Discovery, targetGVR.Resource, 2*time.Second); err != nil {
					return fmt.Errorf("wait for instance resource: %w", err)
				}
			}

			ctx, stop := signalContext()
			defer stop()

			fmt.Printf("Creating %d instances of %s at %d/sec in namespace %s starting at index %d\n", total, targetName, rate, namespace, startIndex)
			result, err := stressutil.CreateResources(
				ctx,
				clients.Dynamic,
				targetGVR,
				namespace,
				total,
				rate,
				func(index int) *unstructured.Unstructured {
					return baseGenerator(startIndex + index)
				},
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
	createCmd.Flags().IntVar(&startIndex, "start-index", 0, "starting instance index for generated names")
	createCmd.Flags().StringVar(&hierarchy, "hierarchy", "", "hierarchy preset whose top-level instances should be created")
	createCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for generated instances")
	createCmd.Flags().StringVar(&prefix, "prefix", "krostress", "resource prefix to target")
	createCmd.Flags().BoolVar(&waitRGD, "wait-rgd", true, "wait for the generated RGD to become Active before creating instances")
	createCmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 5*time.Minute, "timeout when waiting for the RGD and its CRD")

	cleanupCmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete stress-test instances for a generated RGD or hierarchy root",
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
			targetGVR := stressutil.InstanceGVR(prefix, rgdIndex)
			targetName := stressutil.InstanceKind(prefix, rgdIndex)
			if hierarchy != "" {
				rootGVR, rootDefinition, err := stressutil.HierarchyRootGVR(hierarchy)
				if err != nil {
					return fmt.Errorf("resolve hierarchy %q: %w", hierarchy, err)
				}
				targetGVR = rootGVR
				targetName = rootDefinition.SchemaKind
				selector = fmt.Sprintf("%s=%s,kro.run/resource-graph-definition-name=%s", stressutil.TestLabelKey, stressutil.TestLabelValue, rootDefinition.ResourceGraphName)
				if strings.TrimSpace(prefix) != "" {
					selector = fmt.Sprintf("%s,%s=%s", selector, stressutil.PrefixLabelKey, strings.TrimSpace(strings.ToLower(prefix)))
				}
			}

			if sweepOnly && !cleanupOwnedChildren {
				return fmt.Errorf("--sweep-only requires --cleanup-owned-children")
			}

			if strings.TrimSpace(instanceName) != "" {
				if !sweepOnly {
					fmt.Printf("Deleting instance %s/%s of %s...\n", namespace, instanceName, targetName)
					deleteErr := clients.Dynamic.Resource(targetGVR).Namespace(namespace).Delete(context.Background(), instanceName, metav1.DeleteOptions{})
					result := &stressutil.Result{Total: 1}
					switch {
					case deleteErr == nil:
						result.Created = 1
					case apierrors.IsNotFound(deleteErr):
						result.Failed = 1
						result.Errors = append(result.Errors, deleteErr.Error())
					default:
						return deleteErr
					}
					printDeleteResult("instances", result)
				} else {
					fmt.Printf("Sweeping child resources for instance %q in namespace %s without deleting the root...\n", instanceName, namespace)
				}
				if cleanupOwnedChildren {
					fmt.Printf("Deleting child instance resources for instance %q in namespace %s...\n", instanceName, namespace)
					childResult, err := stressutil.DeleteChildInstanceResourcesByPrefix(
						context.Background(),
						clients.Discovery,
						clients.Dynamic,
						namespace,
						instanceName,
						deleteWorkers,
						ownedCleanupWorkers,
						targetGVR,
					)
					if err != nil {
						return err
					}
					printDeleteResult("child instance resources", childResult)

					fmt.Printf("Deleting KRO-owned child resources for instance %q in namespace %s...\n", instanceName, namespace)
					ownedResult, err := stressutil.DeleteOwnedResourcesByInstancePrefix(
						context.Background(),
						clients.Discovery,
						clients.Dynamic,
						namespace,
						instanceName,
						deleteWorkers,
						ownedCleanupWorkers,
						targetGVR,
					)
					if err != nil {
						return err
					}
					printDeleteResult("owned child resources", ownedResult)
				}
				return nil
			}

			if !sweepOnly {
				fmt.Printf("Deleting instances of %s with selector %q in namespace %s...\n", targetName, selector, namespace)
				result, err := stressutil.DeleteResources(context.Background(), clients.Dynamic, targetGVR, namespace, selector, deleteWorkers)
				if err != nil {
					return err
				}
				printDeleteResult("instances", result)
			} else {
				fmt.Printf("Sweeping child resources for prefix %q in namespace %s without deleting the roots...\n", prefix, namespace)
			}
			if cleanupOwnedChildren {
				fmt.Printf("Deleting child instance resources for prefix %q in namespace %s...\n", prefix, namespace)
				childResult, err := stressutil.DeleteChildInstanceResourcesByPrefix(
					context.Background(),
					clients.Discovery,
					clients.Dynamic,
					namespace,
					prefix,
					deleteWorkers,
					ownedCleanupWorkers,
					targetGVR,
				)
				if err != nil {
					return err
				}
				printDeleteResult("child instance resources", childResult)

				fmt.Printf("Deleting KRO-owned child resources for prefix %q in namespace %s...\n", prefix, namespace)
				ownedResult, err := stressutil.DeleteOwnedResourcesByInstancePrefix(
					context.Background(),
					clients.Discovery,
					clients.Dynamic,
					namespace,
					prefix,
					deleteWorkers,
					ownedCleanupWorkers,
					targetGVR,
				)
				if err != nil {
					return err
				}
				printDeleteResult("owned child resources", ownedResult)
			}
			return nil
		},
	}

	cleanupCmd.Flags().IntVar(&rgdIndex, "rgd-index", 0, "generated RGD index to target")
	cleanupCmd.Flags().StringVar(&hierarchy, "hierarchy", "", "hierarchy preset whose top-level instances should be deleted")
	cleanupCmd.Flags().StringVar(&instanceName, "name", "", "exact instance name to delete")
	cleanupCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for generated instances")
	cleanupCmd.Flags().StringVar(&prefix, "prefix", "krostress", "resource prefix to target")
	cleanupCmd.Flags().BoolVar(&cleanupOwnedChildren, "cleanup-owned-children", false, "also delete child instance CRs and KRO-owned resources that match the instance prefix")
	cleanupCmd.Flags().BoolVar(&sweepOnly, "sweep-only", false, "skip deleting the selected root instances and only sweep matching child instance CRs and owned resources")
	cleanupCmd.Flags().IntVar(&deleteWorkers, "delete-workers", 50, "maximum parallel delete requests per resource kind")
	cleanupCmd.Flags().IntVar(&ownedCleanupWorkers, "owned-cleanup-workers", 16, "maximum number of owned resource kinds to sweep in parallel")

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
