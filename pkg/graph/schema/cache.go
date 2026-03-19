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

package schema

import (
	"sync"

	"k8s.io/kube-openapi/pkg/validation/spec"
)

type childSchemaCacheKey struct {
	parent *spec.Schema
	field  string
}

var (
	childSchemaCache sync.Map // key: childSchemaCacheKey, value: *spec.Schema
	listSchemaCache  sync.Map // key: *spec.Schema, value: *spec.Schema
)

const additionalPropertiesCacheField = "__additional_properties__"

// LookupFieldSchema returns a stable pointer for a child field schema.
// Map-backed OpenAPI properties store child schemas by value, so repeated lookups
// would otherwise allocate a fresh copy and defeat pointer-based caches.
func LookupFieldSchema(parent *spec.Schema, field string) *spec.Schema {
	if parent == nil || parent.Properties == nil {
		return nil
	}

	prop, ok := parent.Properties[field]
	if !ok {
		return nil
	}

	key := childSchemaCacheKey{parent: parent, field: field}
	if cached, ok := childSchemaCache.Load(key); ok {
		return cached.(*spec.Schema)
	}

	child := prop
	actual, _ := childSchemaCache.LoadOrStore(key, &child)
	return actual.(*spec.Schema)
}

// LookupAdditionalPropertiesSchema returns a stable schema pointer for
// additionalProperties.
func LookupAdditionalPropertiesSchema(parent *spec.Schema) *spec.Schema {
	if parent == nil || parent.AdditionalProperties == nil {
		return nil
	}

	if parent.AdditionalProperties.Schema != nil {
		return parent.AdditionalProperties.Schema
	}

	if !parent.AdditionalProperties.Allows {
		return nil
	}

	key := childSchemaCacheKey{parent: parent, field: additionalPropertiesCacheField}
	if cached, ok := childSchemaCache.Load(key); ok {
		return cached.(*spec.Schema)
	}

	empty := &spec.Schema{}
	actual, _ := childSchemaCache.LoadOrStore(key, empty)
	return actual.(*spec.Schema)
}
