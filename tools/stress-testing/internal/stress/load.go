package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

type Result struct {
	Total      int           `json:"total"`
	Created    int           `json:"created"`
	Failed     int           `json:"failed"`
	Duration   time.Duration `json:"duration"`
	Rate       float64       `json:"rate"`
	AvgLatency time.Duration `json:"avgLatency"`
	Errors     []string      `json:"errors,omitempty"`
}

type Progress struct {
	Created int
	Failed  int
	Total   int
	Rate    float64
	Elapsed time.Duration
}

func CreateResources(
	ctx context.Context,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
	total int,
	rate int,
	generator func(index int) *unstructured.Unstructured,
	onProgress func(Progress),
) (*Result, error) {
	if total < 0 {
		return nil, fmt.Errorf("total must be >= 0")
	}
	if rate <= 0 {
		return nil, fmt.Errorf("rate must be > 0")
	}

	result := &Result{Total: total}
	start := time.Now()

	if total == 0 {
		return result, nil
	}

	interval := time.Duration(float64(time.Second) / float64(rate))
	if interval <= 0 {
		interval = time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		totalLatency time.Duration
	)

	for i := 0; i < total; i++ {
		select {
		case <-ctx.Done():
			wg.Wait()
			result.Duration = time.Since(start)
			if result.Duration > 0 {
				result.Rate = float64(result.Created) / result.Duration.Seconds()
			}
			return result, ctx.Err()
		case <-ticker.C:
		}

		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			obj := generator(index)
			createStart := time.Now()

			var err error
			if namespace == "" {
				_, err = client.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
			} else {
				_, err = client.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
			}

			latency := time.Since(createStart)

			mu.Lock()
			if err != nil {
				result.Failed++
				if len(result.Errors) < 10 {
					result.Errors = append(result.Errors, err.Error())
				}
			} else {
				result.Created++
				totalLatency += latency
			}

			progress := Progress{
				Created: result.Created,
				Failed:  result.Failed,
				Total:   total,
				Elapsed: time.Since(start),
			}
			if progress.Elapsed > 0 {
				progress.Rate = float64(result.Created+result.Failed) / progress.Elapsed.Seconds()
			}
			shouldReport := onProgress != nil && (result.Created+result.Failed)%10 == 0
			mu.Unlock()

			if shouldReport {
				onProgress(progress)
			}
		}(i)
	}

	wg.Wait()

	result.Duration = time.Since(start)
	if result.Duration > 0 {
		result.Rate = float64(result.Created) / result.Duration.Seconds()
	}
	if result.Created > 0 {
		result.AvgLatency = totalLatency / time.Duration(result.Created)
	}

	return result, nil
}

func DeleteResources(
	ctx context.Context,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
	labelSelector string,
	batchSize int,
) (*Result, error) {
	listOptions := metav1.ListOptions{LabelSelector: labelSelector}

	var (
		list *unstructured.UnstructuredList
		err  error
	)

	if namespace == "" {
		list, err = client.Resource(gvr).List(ctx, listOptions)
	} else {
		list, err = client.Resource(gvr).Namespace(namespace).List(ctx, listOptions)
	}
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}

	result := &Result{Total: len(list.Items)}
	if len(list.Items) == 0 {
		return result, nil
	}

	if batchSize <= 0 {
		batchSize = 50
	}

	start := time.Now()
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for i := 0; i < len(list.Items); i += batchSize {
		end := i + batchSize
		if end > len(list.Items) {
			end = len(list.Items)
		}

		for j := i; j < end; j++ {
			item := list.Items[j]
			wg.Add(1)
			go func(obj unstructured.Unstructured) {
				defer wg.Done()

				targetNamespace := namespace
				if obj.GetNamespace() != "" {
					targetNamespace = obj.GetNamespace()
				}

				var err error
				if targetNamespace == "" {
					err = client.Resource(gvr).Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
				} else {
					err = client.Resource(gvr).Namespace(targetNamespace).Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
				}

				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					result.Failed++
					if len(result.Errors) < 10 {
						result.Errors = append(result.Errors, err.Error())
					}
					return
				}
				result.Created++
			}(item)
		}

		wg.Wait()
	}

	result.Duration = time.Since(start)
	if result.Duration > 0 {
		result.Rate = float64(result.Created) / result.Duration.Seconds()
	}
	return result, nil
}

func ApplyResources(
	ctx context.Context,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
	objects []*unstructured.Unstructured,
	fieldManager string,
) (*Result, error) {
	if fieldManager == "" {
		fieldManager = "krostress"
	}

	result := &Result{Total: len(objects)}
	if len(objects) == 0 {
		return result, nil
	}

	start := time.Now()
	force := true

	for _, original := range objects {
		obj := original.DeepCopy()
		unstructured.RemoveNestedField(obj.Object, "status")

		raw, err := json.Marshal(obj.Object)
		if err != nil {
			result.Failed++
			if len(result.Errors) < 10 {
				result.Errors = append(result.Errors, err.Error())
			}
			continue
		}

		applyStart := time.Now()
		targetNamespace := namespace
		if obj.GetNamespace() != "" {
			targetNamespace = obj.GetNamespace()
		}

		if targetNamespace == "" {
			_, err = client.Resource(gvr).Patch(
				ctx,
				obj.GetName(),
				types.ApplyPatchType,
				raw,
				metav1.PatchOptions{FieldManager: fieldManager, Force: &force},
			)
		} else {
			_, err = client.Resource(gvr).Namespace(targetNamespace).Patch(
				ctx,
				obj.GetName(),
				types.ApplyPatchType,
				raw,
				metav1.PatchOptions{FieldManager: fieldManager, Force: &force},
			)
		}

		if err != nil {
			result.Failed++
			if len(result.Errors) < 10 {
				result.Errors = append(result.Errors, err.Error())
			}
			continue
		}

		result.Created++
		result.AvgLatency += time.Since(applyStart)
	}

	result.Duration = time.Since(start)
	if result.Duration > 0 {
		result.Rate = float64(result.Created) / result.Duration.Seconds()
	}
	if result.Created > 0 {
		result.AvgLatency = result.AvgLatency / time.Duration(result.Created)
	}

	return result, nil
}

func WaitForRGDActive(ctx context.Context, client dynamic.Interface, name string, pollInterval time.Duration) error {
	return WaitForResourceState(ctx, client, RGDGVR, "", name, "Active", pollInterval)
}

func WaitForResourceState(
	ctx context.Context,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
	name string,
	wantState string,
	pollInterval time.Duration,
) error {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	return wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		var (
			obj *unstructured.Unstructured
			err error
		)
		if namespace == "" {
			obj, err = client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
		} else {
			obj, err = client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		}
		if err != nil {
			return false, nil
		}

		state, found, err := unstructured.NestedString(obj.Object, "status", "state")
		if err != nil || !found {
			return false, nil
		}

		return state == wantState, nil
	})
}

func WaitForInstanceResource(ctx context.Context, discoveryClient discovery.DiscoveryInterface, prefix string, rgdIndex int, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	resourceName := InstanceGVR(prefix, rgdIndex).Resource
	return wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		resourceList, err := discoveryClient.ServerResourcesForGroupVersion("kro.run/v1alpha1")
		if err != nil {
			return false, nil
		}

		for _, resource := range resourceList.APIResources {
			if resource.Name == resourceName {
				return true, nil
			}
		}

		return false, nil
	})
}
