package stress

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

const defaultOwnedCleanupWorkers = 16

type cleanupTargetScope int

const (
	cleanupTargetScopeChildInstances cleanupTargetScope = iota
	cleanupTargetScopeOwnedLeaves
)

type cleanupTarget struct {
	gvr           schema.GroupVersionResource
	labelSelector string
	priority      int
}

func DeleteChildInstanceResourcesByPrefix(
	ctx context.Context,
	discoveryClient discovery.DiscoveryInterface,
	client dynamic.Interface,
	namespace string,
	instancePrefix string,
	deleteWorkers int,
	targetWorkers int,
	skipGVRs ...schema.GroupVersionResource,
) (*Result, error) {
	return deleteResourcesByInstancePrefix(
		ctx,
		discoveryClient,
		client,
		namespace,
		instancePrefix,
		deleteWorkers,
		targetWorkers,
		cleanupTargetScopeChildInstances,
		skipGVRs...,
	)
}

func DeleteOwnedResourcesByInstancePrefix(
	ctx context.Context,
	discoveryClient discovery.DiscoveryInterface,
	client dynamic.Interface,
	namespace string,
	instancePrefix string,
	deleteWorkers int,
	targetWorkers int,
	skipGVRs ...schema.GroupVersionResource,
) (*Result, error) {
	return deleteResourcesByInstancePrefix(
		ctx,
		discoveryClient,
		client,
		namespace,
		instancePrefix,
		deleteWorkers,
		targetWorkers,
		cleanupTargetScopeOwnedLeaves,
		skipGVRs...,
	)
}

func deleteResourcesByInstancePrefix(
	ctx context.Context,
	discoveryClient discovery.DiscoveryInterface,
	client dynamic.Interface,
	namespace string,
	instancePrefix string,
	deleteWorkers int,
	targetWorkers int,
	scope cleanupTargetScope,
	skipGVRs ...schema.GroupVersionResource,
) (*Result, error) {
	resourceLists, err := discoveryClient.ServerPreferredNamespacedResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, fmt.Errorf("discover namespaced resources: %w", err)
	}

	skipSet := make(map[schema.GroupVersionResource]struct{}, len(skipGVRs))
	for _, gvr := range skipGVRs {
		skipSet[gvr] = struct{}{}
	}

	result := &Result{}
	start := time.Now()
	targets := buildCleanupTargets(resourceLists, skipSet, scope)
	if err := cleanupTargets(ctx, client, namespace, instancePrefix, deleteWorkers, targetWorkers, targets, result); err != nil {
		return nil, err
	}

	result.Duration = time.Since(start)
	if result.Duration > 0 {
		result.Rate = float64(result.Created) / result.Duration.Seconds()
	}
	return result, nil
}

func buildCleanupTargets(
	resourceLists []*metav1.APIResourceList,
	skipSet map[schema.GroupVersionResource]struct{},
	scope cleanupTargetScope,
) []cleanupTarget {
	targets := make([]cleanupTarget, 0, len(resourceLists)*4)
	ownedSelector := fmt.Sprintf("%s=true", krometadata.OwnedLabel)

	for _, resourceList := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, apiResource := range resourceList.APIResources {
			if strings.Contains(apiResource.Name, "/") {
				continue
			}
			if !supportsVerb(apiResource.Verbs, "list") || !supportsVerb(apiResource.Verbs, "delete") {
				continue
			}

			gvr := schema.GroupVersionResource{
				Group:    groupVersion.Group,
				Version:  groupVersion.Version,
				Resource: apiResource.Name,
			}
			if _, skip := skipSet[gvr]; skip {
				continue
			}
			if gvr.Group == "kro.run" && gvr.Resource == "resourcegraphdefinitions" {
				continue
			}
			switch scope {
			case cleanupTargetScopeChildInstances:
				if gvr.Group != "kro.run" {
					continue
				}
			case cleanupTargetScopeOwnedLeaves:
				if gvr.Group == "kro.run" {
					continue
				}
			}

			target := cleanupTarget{
				gvr:           gvr,
				labelSelector: ownedSelector,
				priority:      cleanupPriorityForGVR(gvr),
			}
			if gvr.Group == "kro.run" {
				// Child instance CRs are the hottest path during hierarchy cleanup.
				// Listing them directly by kind is much faster than waiting for the
				// generic owned-resource sweep to reach them.
				target.labelSelector = ""
			}

			targets = append(targets, target)
		}
	}

	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].priority != targets[j].priority {
			return targets[i].priority < targets[j].priority
		}
		return targets[i].gvr.String() < targets[j].gvr.String()
	})

	return targets
}

func cleanupPriorityForGVR(gvr schema.GroupVersionResource) int {
	switch {
	case gvr.Group == "kro.run":
		return 0
	case isPreferredOwnedCleanupGVR(gvr):
		return 1
	default:
		return 2
	}
}

func isPreferredOwnedCleanupGVR(gvr schema.GroupVersionResource) bool {
	switch {
	case gvr.Group == "" && (gvr.Resource == "configmaps" || gvr.Resource == "secrets" || gvr.Resource == "serviceaccounts"):
		return true
	case gvr.Group == "rbac.authorization.k8s.io" && (gvr.Resource == "roles" || gvr.Resource == "rolebindings"):
		return true
	case gvr.Group == "apps" && gvr.Resource == "deployments":
		return true
	default:
		return false
	}
}

func cleanupTargets(
	ctx context.Context,
	client dynamic.Interface,
	namespace string,
	instancePrefix string,
	deleteWorkers int,
	targetWorkers int,
	targets []cleanupTarget,
	result *Result,
) error {
	if len(targets) == 0 {
		return nil
	}

	workerCount := targetWorkers
	if workerCount <= 0 {
		workerCount = defaultOwnedCleanupWorkers
	}
	if len(targets) < workerCount {
		workerCount = len(targets)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	targetCh := make(chan cleanupTarget)
	errCh := make(chan error, 1)
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range targetCh {
				local, err := cleanupTargetMatches(ctx, client, namespace, instancePrefix, deleteWorkers, target)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if local == nil {
					continue
				}

				mu.Lock()
				result.Total += local.Total
				result.Created += local.Created
				result.Failed += local.Failed
				for _, itemErr := range local.Errors {
					if len(result.Errors) >= 10 {
						break
					}
					result.Errors = append(result.Errors, itemErr)
				}
				mu.Unlock()
			}
		}()
	}

	for _, target := range targets {
		select {
		case err := <-errCh:
			close(targetCh)
			wg.Wait()
			return err
		case targetCh <- target:
		}
	}
	close(targetCh)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func cleanupTargetMatches(
	ctx context.Context,
	client dynamic.Interface,
	namespace string,
	instancePrefix string,
	deleteWorkers int,
	target cleanupTarget,
) (*Result, error) {
	list, err := listCleanupTarget(ctx, client, namespace, target)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		result := &Result{Failed: 1}
		result.Errors = append(result.Errors, fmt.Sprintf("list %s: %v", target.gvr.String(), err))
		return result, nil
	}

	filtered := filterOwnedCleanupMatches(list.Items, instancePrefix)
	if len(filtered) == 0 {
		return nil, nil
	}

	local := &Result{Total: len(filtered)}
	deleteObjectsConcurrently(ctx, client, target.gvr, namespace, filtered, deleteWorkers, true, local)
	return local, nil
}

func listCleanupTarget(
	ctx context.Context,
	client dynamic.Interface,
	namespace string,
	target cleanupTarget,
) (*unstructured.UnstructuredList, error) {
	listOptions := metav1.ListOptions{LabelSelector: target.labelSelector}
	if namespace == "" {
		return client.Resource(target.gvr).List(ctx, listOptions)
	}
	return client.Resource(target.gvr).Namespace(namespace).List(ctx, listOptions)
}

func filterOwnedCleanupMatches(items []unstructured.Unstructured, instancePrefix string) []unstructured.Unstructured {
	if instancePrefix == "" {
		return items
	}

	prefix := sanitizePrefix(instancePrefix)
	filtered := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		labels := item.GetLabels()
		if matchesCleanupPrefix(labels[krometadata.InstanceLabel], prefix) || matchesCleanupPrefix(item.GetName(), prefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func matchesCleanupPrefix(value string, prefix string) bool {
	if value == "" || prefix == "" {
		return false
	}
	return value == prefix || strings.HasPrefix(value, prefix+"-")
}

func supportsVerb(verbs []string, want string) bool {
	for _, verb := range verbs {
		if verb == want {
			return true
		}
	}
	return false
}
