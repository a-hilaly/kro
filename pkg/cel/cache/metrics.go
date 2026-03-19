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

package cache

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	cacheDeclTypes     = "decl_types"
	cacheNamedTypes    = "named_types"
	cacheTypedEnvs     = "typed_envs"
	cacheFieldTypeMaps = "field_type_maps"
	cacheCheckedASTs   = "checked_asts"
	cachePrograms      = "programs"
)

var (
	builderCacheRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cel_builder_cache_requests_total",
			Help: "Total number of builder cache requests by cache and result.",
		},
		[]string{"cache", "result"},
	)

	builderCacheEntries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cel_builder_cache_entries",
			Help: "Current number of entries stored in each builder cache.",
		},
		[]string{"cache"},
	)

	builderCacheFillDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cel_builder_cache_fill_duration_seconds",
			Help:    "Duration spent computing a builder cache entry on cache miss.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"cache"},
	)

	sessionCacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cel_cache_session_hits_total",
			Help: "Total number of session cache hits",
		},
		[]string{"cache_type"},
	)
	sessionCacheMissesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cel_cache_session_misses_total",
			Help: "Total number of session cache misses",
		},
		[]string{"cache_type"},
	)
	sessionCacheASTReuseTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cel_cache_session_ast_reuse_total",
			Help: "Total number of checked ASTs reused during program compilation (skipping parse+check)",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		builderCacheRequestsTotal,
		builderCacheEntries,
		builderCacheFillDuration,
		sessionCacheHitsTotal,
		sessionCacheMissesTotal,
		sessionCacheASTReuseTotal,
	)
}

func recordBuilderCacheHit(cacheName string) {
	builderCacheRequestsTotal.WithLabelValues(cacheName, "hit").Inc()
}

func recordBuilderCacheMiss(cacheName string) {
	builderCacheRequestsTotal.WithLabelValues(cacheName, "miss").Inc()
}

func recordBuilderCacheError(cacheName string) {
	builderCacheRequestsTotal.WithLabelValues(cacheName, "error").Inc()
}
