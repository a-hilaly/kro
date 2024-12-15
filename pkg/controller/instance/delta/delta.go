// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package delta

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Difference represents a single difference between two objects
type Difference struct {
	Path     string      `json:"path"`
	Desired  interface{} `json:"desired"`
	Observed interface{} `json:"observed"`
}

// Compare compares desired and observed unstructured objects.
// Returns a slice of Differences for fields that differ.
func Compare(desired, observed *unstructured.Unstructured) ([]Difference, error) {
	desiredCopy := desired.DeepCopy()
	observedCopy := observed.DeepCopy()

	cleanMetadata(desiredCopy)
	cleanMetadata(observedCopy)

	var differences []Difference
	walkCompare(desiredCopy.Object, observedCopy.Object, "", &differences)
	return differences, nil
}

func cleanMetadata(obj *unstructured.Unstructured) {
	metadata, ok := obj.Object["metadata"].(map[string]interface{})
	if !ok {
		return
	}

	if annotations, exists := metadata["annotations"].(map[string]interface{}); exists {
		if len(annotations) == 0 {
			delete(metadata, "annotations")
		}
	}

	if annotations, exists := metadata["labels"].(map[string]interface{}); exists {
		if len(annotations) == 0 {
			delete(metadata, "labels")
		}
	}

	fieldsToRemove := []string{
		"creationTimestamp",
		"deletionTimestamp",
		"generation",
		"resourceVersion",
		"selfLink",
		"uid",
		"managedFields",
		"ownerReferences",
		"finalizers",
	}

	for _, field := range fieldsToRemove {
		delete(metadata, field)
	}
}

func walkCompare(desired, observed interface{}, path string, differences *[]Difference) {
	switch d := desired.(type) {
	case map[string]interface{}:
		e, ok := observed.(map[string]interface{})
		if !ok {
			*differences = append(*differences, Difference{
				Path:     path,
				Observed: observed,
				Desired:  desired,
			})
			return
		}
		walkMap(d, e, path, differences)

	case []interface{}:
		e, ok := observed.([]interface{})
		if !ok {
			*differences = append(*differences, Difference{
				Path:     path,
				Observed: observed,
				Desired:  desired,
			})
			return
		}
		walkSlice(d, e, path, differences)

	default:
		if desired != observed {
			*differences = append(*differences, Difference{
				Path:     path,
				Observed: observed,
				Desired:  desired,
			})
		}
	}
}

func walkMap(desired, observed map[string]interface{}, path string, differences *[]Difference) {
	for k, desiredVal := range desired {
		newPath := k
		if path != "" {
			newPath = fmt.Sprintf("%s.%s", path, k)
		}

		observedVal, exists := observed[k]
		if !exists && desiredVal != nil {
			*differences = append(*differences, Difference{
				Path:     newPath,
				Observed: nil,
				Desired:  desiredVal,
			})
			continue
		}

		walkCompare(desiredVal, observedVal, newPath, differences)
	}
}

func walkSlice(desired, observed []interface{}, path string, differences *[]Difference) {
	if len(desired) != len(observed) {
		*differences = append(*differences, Difference{
			Path:     path,
			Observed: observed,
			Desired:  desired,
		})
		return
	}

	for i := range desired {
		newPath := fmt.Sprintf("%s[%d]", path, i)
		walkCompare(desired[i], observed[i], newPath, differences)
	}
}
