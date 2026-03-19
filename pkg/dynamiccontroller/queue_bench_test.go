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

import (
	"fmt"
	"testing"

	"k8s.io/client-go/util/workqueue"
)

// Queue[T] interface — Push, Pop, Touch, Len
type benchQueue interface {
	Push(item string)
	Pop() string
	Touch(item string)
	Len() int
}

// Wrap default FIFO queue
type fifoWrapper struct {
	q workqueue.Queue[string]
}

func (w *fifoWrapper) Push(item string) { w.q.Push(item) }
func (w *fifoWrapper) Pop() string      { return w.q.Pop() }
func (w *fifoWrapper) Touch(item string) { w.q.Touch(item) }
func (w *fifoWrapper) Len() int         { return w.q.Len() }

func benchPushPop(b *testing.B, name string, q benchQueue, size int) {
	b.Run(fmt.Sprintf("%s/PushPop/%d", name, size), func(b *testing.B) {
		// Pre-fill
		for i := 0; i < size; i++ {
			q.Push(fmt.Sprintf("item-%d", i))
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			item := fmt.Sprintf("bench-%d", i)
			q.Push(item)
			q.Pop()
		}
	})
}

func benchPushTouchPop(b *testing.B, name string, q benchQueue, size int) {
	b.Run(fmt.Sprintf("%s/PushTouchPop/%d", name, size), func(b *testing.B) {
		// Pre-fill
		for i := 0; i < size; i++ {
			q.Push(fmt.Sprintf("item-%d", i))
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			item := fmt.Sprintf("bench-%d", i)
			q.Push(item)
			q.Touch(item)
			q.Pop()
		}
	})
}

func benchDemotionCycle(b *testing.B, name string, q benchQueue, size int) {
	b.Run(fmt.Sprintf("%s/DemotionCycle/%d", name, size), func(b *testing.B) {
		// Pre-fill with items that have been pushed multiple times (simulating failures)
		for i := 0; i < size; i++ {
			item := fmt.Sprintf("item-%d", i)
			q.Push(item)
			q.Pop()
			q.Push(item)
			q.Pop()
			q.Push(item) // third push, should be demoted
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Add fresh item and pop — fresh should come first in MLFQ
			item := fmt.Sprintf("fresh-%d", i)
			q.Push(item)
			q.Pop()
		}
	})
}

func BenchmarkQueues(b *testing.B) {
	for _, size := range []int{100, 10000, 100000, 500000} {
		// FIFO (default)
		benchPushPop(b, "FIFO", &fifoWrapper{workqueue.DefaultQueue[string]()}, size)
		// Heat Queue
		benchPushPop(b, "Heat", NewHeatQueue[string](), size)
		// Fair Queue
		benchPushPop(b, "Fair", NewFairQueue(func(s string) string { return s[:4] }), size)
		// MLFQ
		benchPushPop(b, "MLFQ", NewMLFQueue[string](), size)

		// With Touch
		benchPushTouchPop(b, "FIFO", &fifoWrapper{workqueue.DefaultQueue[string]()}, size)
		benchPushTouchPop(b, "Heat", NewHeatQueue[string](), size)
		benchPushTouchPop(b, "Fair", NewFairQueue(func(s string) string { return s[:4] }), size)
		benchPushTouchPop(b, "MLFQ", NewMLFQueue[string](), size)

		// Demotion cycle (only meaningful for MLFQ and Heat)
		benchDemotionCycle(b, "FIFO", &fifoWrapper{workqueue.DefaultQueue[string]()}, size)
		benchDemotionCycle(b, "Heat", NewHeatQueue[string](), size)
		benchDemotionCycle(b, "MLFQ", NewMLFQueue[string](), size)
	}
}
