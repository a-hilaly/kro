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

// FairQueue is a workqueue.Queue[T] implementation that provides per-key
// fair scheduling via round-robin. Items are routed to sub-queues by a
// key function. Pop round-robins across non-empty sub-queues so no single
// key can starve others regardless of how many items it requeues.
//
// For kro, the key is the GVR — this ensures leaf GVRs get equal processing
// time as parent GVRs, even when parents requeue 20x per instance.
type FairQueue[T comparable] struct {
	keyFn  func(T) string
	queues map[string][]T
	keys   []string
	robin  int
	total  int
}

// NewFairQueue creates a new FairQueue with the given key extraction function.
func NewFairQueue[T comparable](keyFn func(T) string) *FairQueue[T] {
	return &FairQueue[T]{
		keyFn:  keyFn,
		queues: make(map[string][]T),
	}
}

// Push adds an item to the sub-queue for its key.
func (q *FairQueue[T]) Push(item T) {
	key := q.keyFn(item)
	if _, ok := q.queues[key]; !ok {
		q.keys = append(q.keys, key)
	}
	q.queues[key] = append(q.queues[key], item)
	q.total++
}

// Pop returns the next item using round-robin across non-empty sub-queues.
func (q *FairQueue[T]) Pop() T {
	for i := 0; i < len(q.keys); i++ {
		idx := (q.robin + i) % len(q.keys)
		key := q.keys[idx]
		if len(q.queues[key]) > 0 {
			item := q.queues[key][0]
			// Clear reference for GC.
			var zero T
			q.queues[key][0] = zero
			q.queues[key] = q.queues[key][1:]
			q.robin = (idx + 1) % len(q.keys)
			q.total--
			return item
		}
	}
	var zero T
	return zero
}

// Touch is called when an already-queued item is re-added.
// No-op for fair queue — round-robin provides fairness without priority.
func (q *FairQueue[T]) Touch(item T) {}

// Len returns the total number of items across all sub-queues.
func (q *FairQueue[T]) Len() int {
	return q.total
}
