package stress

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type SyntheticRGDPlan struct {
	TotalRGDs            int
	TotalResources       int
	AverageResources     float64
	MinResources         int
	MinResourcesIndex    int
	MaxResources         int
	MaxResourcesIndex    int
	TotalStatusLeaves    int
	AverageStatusLeaves  float64
	MinStatusLeaves      int
	MinStatusLeavesIndex int
	MaxStatusLeaves      int
	MaxStatusLeavesIndex int
	UniqueResourceShapes int
	UniqueStatusShapes   int
}

func PlanSyntheticRGDs(prefix string, cfg Complexity, total int) (SyntheticRGDPlan, error) {
	checkpoints, err := PlanSyntheticRGDCheckpoints(prefix, cfg, total, total)
	if err != nil {
		return SyntheticRGDPlan{}, err
	}
	if len(checkpoints) == 0 {
		return SyntheticRGDPlan{}, nil
	}
	return checkpoints[len(checkpoints)-1], nil
}

func PlanSyntheticRGDCheckpoints(prefix string, cfg Complexity, total int, step int) ([]SyntheticRGDPlan, error) {
	if total < 0 {
		return nil, fmt.Errorf("total must be >= 0")
	}
	if total == 0 {
		return nil, nil
	}
	if step <= 0 {
		return nil, fmt.Errorf("step must be > 0")
	}

	generate := RGDGenerator(prefix, cfg)
	plan := SyntheticRGDPlan{
		MinResources:         math.MaxInt,
		MinStatusLeaves:      math.MaxInt,
		MinResourcesIndex:    -1,
		MaxResourcesIndex:    -1,
		MinStatusLeavesIndex: -1,
		MaxStatusLeavesIndex: -1,
	}

	resourceShapes := map[string]struct{}{}
	statusShapes := map[string]struct{}{}
	checkpoints := make([]SyntheticRGDPlan, 0, (total+step-1)/step)

	for i := 0; i < total; i++ {
		rgd := generate(i)

		resources, found, err := unstructured.NestedSlice(rgd.Object, "spec", "resources")
		if err != nil || !found {
			return nil, fmt.Errorf("extract resources for RGD %d: found=%v err=%w", i, found, err)
		}
		resourceCount := len(resources)
		plan.TotalResources += resourceCount
		if resourceCount < plan.MinResources {
			plan.MinResources = resourceCount
			plan.MinResourcesIndex = i
		}
		if resourceCount > plan.MaxResources {
			plan.MaxResources = resourceCount
			plan.MaxResourcesIndex = i
		}

		resourceShape, err := resourceShapeKey(resources)
		if err != nil {
			return nil, fmt.Errorf("resource shape for RGD %d: %w", i, err)
		}
		resourceShapes[resourceShape] = struct{}{}

		statusSchema, found, err := unstructured.NestedMap(rgd.Object, "spec", "schema", "status")
		if err != nil || !found {
			return nil, fmt.Errorf("extract status schema for RGD %d: found=%v err=%w", i, found, err)
		}
		statusLeafCount := countStatusLeaves(statusSchema)
		plan.TotalStatusLeaves += statusLeafCount
		if statusLeafCount < plan.MinStatusLeaves {
			plan.MinStatusLeaves = statusLeafCount
			plan.MinStatusLeavesIndex = i
		}
		if statusLeafCount > plan.MaxStatusLeaves {
			plan.MaxStatusLeaves = statusLeafCount
			plan.MaxStatusLeavesIndex = i
		}

		statusShape, err := json.Marshal(statusSchema)
		if err != nil {
			return nil, fmt.Errorf("status shape for RGD %d: %w", i, err)
		}
		statusShapes[string(statusShape)] = struct{}{}

		current := i + 1
		if current%step == 0 || current == total {
			snapshot := plan
			snapshot.TotalRGDs = current
			snapshot.AverageResources = float64(snapshot.TotalResources) / float64(current)
			snapshot.AverageStatusLeaves = float64(snapshot.TotalStatusLeaves) / float64(current)
			snapshot.UniqueResourceShapes = len(resourceShapes)
			snapshot.UniqueStatusShapes = len(statusShapes)
			checkpoints = append(checkpoints, snapshot)
		}
	}
	return checkpoints, nil
}

func resourceShapeKey(resources []interface{}) (string, error) {
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("unexpected resource type %T", resource)
		}
		id, _ := resourceMap["id"].(string)
		ids = append(ids, id)
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func countStatusLeaves(value interface{}) int {
	switch typed := value.(type) {
	case string:
		return 1
	case []interface{}:
		count := 0
		for _, item := range typed {
			count += countStatusLeaves(item)
		}
		return count
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		count := 0
		for _, key := range keys {
			count += countStatusLeaves(typed[key])
		}
		return count
	default:
		return 0
	}
}
