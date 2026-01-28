// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package simpleschema

import (
	"errors"
	"fmt"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/kubernetes-sigs/kro/pkg/graph/dag"
	"github.com/kubernetes-sigs/kro/pkg/simpleschema/types"
)

// customType stores the schema and required state for a custom type.
type customType struct {
	Schema   extv1.JSONSchemaProps
	Required bool
}

type transformer struct {
	customTypes map[string]customType
}

// Resolve implements types.Resolver.
func (t *transformer) Resolve(name string) (*extv1.JSONSchemaProps, error) {
	ct, ok := t.customTypes[name]
	if !ok {
		return nil, fmt.Errorf("unknown type: %s", name)
	}
	return ct.Schema.DeepCopy(), nil
}

// IsRequired returns whether a custom type has required=true marker.
func (t *transformer) IsRequired(name string) bool {
	ct, ok := t.customTypes[name]
	if !ok {
		return false
	}
	return ct.Required
}

func (t *transformer) loadCustomTypes(customTypes map[string]interface{}) error {
	if len(customTypes) == 0 {
		return nil
	}

	// Parse all types with their markers
	parsed := make(map[string]types.Type)
	markers := make(map[string][]*Marker)
	for name, spec := range customTypes {
		typ, m, err := parseSpec(spec)
		if err != nil {
			return fmt.Errorf("parsing type %s: %w", name, err)
		}
		parsed[name] = typ
		markers[name] = m
	}

	// Build DAG for dependency ordering
	graph := dag.NewDirectedAcyclicGraph[string]()
	for name := range parsed {
		if err := graph.AddVertex(name, 0); err != nil {
			return err
		}
	}
	for name, typ := range parsed {
		if err := graph.AddDependencies(name, typ.Deps()); err != nil {
			var cycleErr *dag.CycleError[string]
			if errors.As(err, &cycleErr) {
				return fmt.Errorf("cyclic dependency in type %s: %w", name, err)
			}
			return err
		}
	}

	// Build schemas in topological order
	order, err := graph.TopologicalSort()
	if err != nil {
		return fmt.Errorf("resolving type order: %w", err)
	}

	for _, name := range order {
		schema, err := parsed[name].Schema(t)
		if err != nil {
			return fmt.Errorf("building schema for %s: %w", name, err)
		}

		// Track required state from markers
		required := false
		for _, m := range markers[name] {
			if m.Key == "required" && m.Value == "true" {
				required = true
			}
		}

		// Apply non-required markers to the schema
		dummyParent := &extv1.JSONSchemaProps{}
		if err := applyMarkers(schema, markers[name], name, dummyParent); err != nil {
			return fmt.Errorf("applying markers for %s: %w", name, err)
		}

		t.customTypes[name] = customType{Schema: *schema, Required: required}
	}

	return nil
}

func (t *transformer) buildSchema(spec map[string]interface{}) (*extv1.JSONSchemaProps, error) {
	schema := &extv1.JSONSchemaProps{
		Type:       "object",
		Properties: make(map[string]extv1.JSONSchemaProps),
	}

	childHasDefault := false

	for fieldName, fieldSpec := range spec {
		fieldSchema, err := t.buildFieldSchema(fieldName, fieldSpec, schema)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", fieldName, err)
		}
		schema.Properties[fieldName] = *fieldSchema

		if fieldSchema.Default != nil {
			childHasDefault = true
		}
	}

	// If any child has a default, set parent default to empty object
	if childHasDefault {
		schema.Default = &extv1.JSON{Raw: []byte("{}")}
	}

	return schema, nil
}

func (t *transformer) buildFieldSchema(name string, spec interface{}, parent *extv1.JSONSchemaProps) (*extv1.JSONSchemaProps, error) {
	switch val := spec.(type) {
	case string:
		return t.buildFieldFromString(name, val, parent)
	case map[string]interface{}:
		return t.buildSchema(val)
	default:
		return nil, fmt.Errorf("unexpected type: %T", spec)
	}
}

func (t *transformer) buildFieldFromString(name, fieldValue string, parent *extv1.JSONSchemaProps) (*extv1.JSONSchemaProps, error) {
	typ, markers, err := ParseField(fieldValue)
	if err != nil {
		return nil, err
	}

	schema, err := typ.Schema(t)
	if err != nil {
		return nil, err
	}

	// Check if this is a custom type that has required=true
	if custom, ok := typ.(types.Custom); ok {
		if t.IsRequired(string(custom)) {
			parent.Required = append(parent.Required, name)
		}
	}

	if err := applyMarkers(schema, markers, name, parent); err != nil {
		return nil, err
	}

	return schema, nil
}
