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

package dynamiccontroller

type coordinatorMetricSummary struct {
	InstanceWatchCountByParent   map[string]int
	ScalarWatchRequestsByGVR     map[string]int
	CollectionWatchRequestsByGVR map[string]int
}

func syncCoordinatorMetrics(summary coordinatorMetricSummary, activeWatchCount int) {
	watchCount.Set(float64(activeWatchCount))

	instanceWatchCount.Reset()
	for parentGVR, count := range summary.InstanceWatchCountByParent {
		instanceWatchCount.WithLabelValues(parentGVR).Set(float64(count))
	}

	watchRequestCount.Reset()
	for gvr, count := range summary.ScalarWatchRequestsByGVR {
		watchRequestCount.WithLabelValues(gvr, "scalar").Set(float64(count))
	}
	for gvr, count := range summary.CollectionWatchRequestsByGVR {
		watchRequestCount.WithLabelValues(gvr, "collection").Set(float64(count))
	}
}

// RecordReconcileFailureAfterRegister increments the post-register reconcile
// failure counter with a normalized reason label.
func RecordReconcileFailureAfterRegister(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	reconcileFailuresAfterRegisterTotal.WithLabelValues(reason).Inc()
}
