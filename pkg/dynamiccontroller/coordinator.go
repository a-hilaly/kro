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
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// InstanceWatcher is the interface the instance reconciler uses to request
// watches. It is scoped to a single instance and obtained via
// WatchCoordinator.ForInstance().
type InstanceWatcher interface {
	// Watch requests that the instance be re-reconciled when the specified
	// resource changes. Call this for every resource (managed or external)
	// the instance cares about.
	//
	// For scalar resources: set Name + Namespace.
	// For collections: set Selector + Namespace.
	//
	// Call Watch() BEFORE operating on the resource to avoid event gaps.
	Watch(req WatchRequest) error

	// Done signals that all Watch() calls for this reconciliation cycle
	// are complete. Any watch requests from the previous cycle that were
	// NOT re-requested are automatically cleaned up. If commit is false, the
	// current cycle is discarded and the previously committed watch set stays
	// active.
	Done(commit bool)
}

// WatchRequest describes a resource the instance reconciler wants to watch.
// For scalar resources, set Name + Namespace.
// For collections, set Selector + Namespace. Selector supports both
// matchLabels and matchExpressions (the full metav1.LabelSelector spec).
type WatchRequest struct {
	// NodeID is the graph node ID (for debugging/metrics).
	NodeID string
	// GVR is the GroupVersionResource to watch.
	GVR schema.GroupVersionResource
	// Name is the specific resource name (scalar watches).
	Name string
	// Namespace is the resource namespace. Empty for cluster-scoped resources.
	Namespace string
	// Selector is a label selector for collection watches. nil means scalar watch.
	Selector labels.Selector
}

// isCollection returns true if this is a selector-based collection watch.
func (r *WatchRequest) isCollection() bool {
	return r.Selector != nil
}

// EnqueueFunc is called by the coordinator to enqueue an instance for
// re-reconciliation when one of its watched resources changes.
type EnqueueFunc func(parentGVR schema.GroupVersionResource, instance types.NamespacedName)

// instanceKey uniquely identifies an instance across all RGDs.
type instanceKey struct {
	parentGVR schema.GroupVersionResource
	instance  types.NamespacedName
}

// instanceState tracks watch requests for a single instance across
// reconciliation cycles. The coordinator uses current vs previous to
// detect and clean up stale requests.
type instanceState struct {
	current  map[string]*WatchRequest // keyed by nodeID
	previous map[string]*WatchRequest // from last Done() cycle
}

// scalarEntry is a single scalar watch in the reverse index.
type scalarEntry struct {
	nodeID string
	key    instanceKey
}

// collectionEntry is a single collection watch in the reverse index.
type collectionEntry struct {
	nodeID    string // enables identity-based matching for dedup and removal
	selector  labels.Selector
	namespace string
	key       instanceKey
}

// parentShard holds instance state for all instances of a single parent GVR.
// Workers reconciling different parent GVRs never contend on the same shard.
type parentShard struct {
	mu        sync.Mutex
	instances map[types.NamespacedName]*instanceState
}

// indexShard holds the reverse index entries for a single child GVR.
// RouteEvent and index updates for different child GVRs never contend.
type indexShard struct {
	mu          sync.RWMutex
	scalars     map[types.NamespacedName][]scalarEntry
	collections []collectionEntry
}

// WatchCoordinator aggregates watch requests from all instances, manages
// shared watches via WatchManager, and routes events back to the correct
// instances.
//
// Uses two-level sharding to minimize lock contention:
//   - parentShards: per-parent-GVR lock for instance state. Workers reconciling
//     different parent GVRs never contend.
//   - indexShards: per-child-GVR lock for reverse indexes. RouteEvent for
//     different child GVRs never contends.
type WatchCoordinator struct {
	watches *WatchManager
	enqueue EnqueueFunc
	log     logr.Logger

	// Per-parentGVR shards for instance state.
	parentShards sync.Map // map[schema.GroupVersionResource]*parentShard

	// Per-childGVR shards for reverse indexes.
	indexShards sync.Map // map[schema.GroupVersionResource]*indexShard
}

// NewWatchCoordinator creates a new WatchCoordinator.
func NewWatchCoordinator(watches *WatchManager, enqueue EnqueueFunc, log logr.Logger) *WatchCoordinator {
	return &WatchCoordinator{
		watches: watches,
		enqueue: enqueue,
		log:     log.WithName("watch-coordinator"),
	}
}

// getParentShard returns (or creates) the shard for a parent GVR.
func (c *WatchCoordinator) getParentShard(gvr schema.GroupVersionResource) *parentShard {
	if v, ok := c.parentShards.Load(gvr); ok {
		return v.(*parentShard)
	}
	s := &parentShard{instances: make(map[types.NamespacedName]*instanceState)}
	actual, _ := c.parentShards.LoadOrStore(gvr, s)
	return actual.(*parentShard)
}

// getIndexShard returns (or creates) the index shard for a child GVR.
func (c *WatchCoordinator) getIndexShard(gvr schema.GroupVersionResource) *indexShard {
	if v, ok := c.indexShards.Load(gvr); ok {
		return v.(*indexShard)
	}
	s := &indexShard{scalars: make(map[types.NamespacedName][]scalarEntry)}
	actual, _ := c.indexShards.LoadOrStore(gvr, s)
	return actual.(*indexShard)
}

// ForInstance returns a scoped InstanceWatcher handle for the given instance.
func (c *WatchCoordinator) ForInstance(parentGVR schema.GroupVersionResource, instance types.NamespacedName) InstanceWatcher {
	return &instanceWatcher{
		coordinator: c,
		parentGVR:   parentGVR,
		instance:    instance,
	}
}

// indexOp describes an add or remove operation on a child GVR's index shard.
type indexOp struct {
	gvr    schema.GroupVersionResource
	add    bool // true = add, false = remove
	key    instanceKey
	req    WatchRequest
	nodeID string
}

// commitWatches registers all buffered watch requests for an instance in a
// single parent-shard lock acquisition, then applies index changes to
// per-childGVR shards. Workers reconciling different parent GVRs never contend.
func (c *WatchCoordinator) commitWatches(key instanceKey, requests []WatchRequest) {
	shard := c.getParentShard(key.parentGVR)
	shard.mu.Lock()

	// If no watches were requested and no prior state exists, nothing to do.
	state, ok := shard.instances[key.instance]
	if !ok && len(requests) == 0 {
		shard.mu.Unlock()
		return
	}
	if !ok {
		state = &instanceState{
			current:  make(map[string]*WatchRequest, len(requests)),
			previous: make(map[string]*WatchRequest),
		}
		shard.instances[key.instance] = state
	}

	// Compute index operations while holding the parent shard lock.
	var ops []indexOp
	var ensureGVRs []schema.GroupVersionResource

	// Process all buffered requests.
	for i := range requests {
		req := &requests[i]

		// Fix reverse index orphaning on nodeID reuse.
		if old, exists := state.current[req.NodeID]; exists {
			if !sameWatchTarget(old, req) {
				if prev, shared := state.previous[req.NodeID]; !shared || !sameWatchTarget(prev, old) {
					ops = append(ops, indexOp{gvr: old.GVR, add: false, key: key, req: *old, nodeID: old.NodeID})
				}
			}
		}

		// Add to current cycle.
		state.current[req.NodeID] = req

		// Add to reverse index only when not covered by previous cycle.
		if prev, shared := state.previous[req.NodeID]; !shared || !sameWatchTarget(prev, req) {
			ops = append(ops, indexOp{gvr: req.GVR, add: true, key: key, req: *req, nodeID: req.NodeID})
			ensureGVRs = append(ensureGVRs, req.GVR)
		}
	}

	// Finalize cycle: remove stale previous requests not in current.
	for nodeID, oldReq := range state.previous {
		if newReq, stillActive := state.current[nodeID]; stillActive && sameWatchTarget(newReq, oldReq) {
			continue
		}
		ops = append(ops, indexOp{gvr: oldReq.GVR, add: false, key: key, req: *oldReq, nodeID: oldReq.NodeID})
	}

	// Swap: previous = current, current = new empty map.
	state.previous = state.current
	state.current = make(map[string]*WatchRequest)

	shard.mu.Unlock()

	// Apply index operations to per-childGVR shards (no parent lock held).
	c.applyIndexOps(ops)

	// Ensure watches, check orphans, and refresh metrics outside all locks.
	c.ensureWatchesBatch(ensureGVRs)
	c.checkAndStopOrphans(ops)
	c.refreshMetrics()
}

// applyIndexOps applies a batch of add/remove operations to the index shards.
func (c *WatchCoordinator) applyIndexOps(ops []indexOp) {
	// Group by child GVR to minimize lock acquisitions.
	byGVR := make(map[schema.GroupVersionResource][]indexOp, len(ops))
	for _, op := range ops {
		byGVR[op.gvr] = append(byGVR[op.gvr], op)
	}

	for gvr, gvrOps := range byGVR {
		idx := c.getIndexShard(gvr)
		idx.mu.Lock()
		for _, op := range gvrOps {
			if op.add {
				if op.req.isCollection() {
					addCollectionEntry(idx, op.key, op.req)
				} else {
					addScalarEntry(idx, op.key, op.req)
				}
			} else {
				if op.req.isCollection() {
					removeCollectionEntry(idx, op.key, &op.req)
				} else {
					removeScalarEntry(idx, op.key, &op.req)
				}
			}
		}
		idx.mu.Unlock()
	}
}

// ensureWatchesBatch calls EnsureWatch for each unique GVR.
func (c *WatchCoordinator) ensureWatchesBatch(gvrs []schema.GroupVersionResource) {
	seen := make(map[schema.GroupVersionResource]struct{}, len(gvrs))
	for _, gvr := range gvrs {
		if _, ok := seen[gvr]; ok {
			continue
		}
		seen[gvr] = struct{}{}
		if err := c.watches.EnsureWatch(gvr); err != nil {
			c.log.Error(err, "Failed to ensure watch", "gvr", gvr)
		}
	}
}

// abortInstance discards the current reconciliation cycle for an instance.
func (c *WatchCoordinator) abortInstance(key instanceKey) {
	shard := c.getParentShard(key.parentGVR)
	shard.mu.Lock()

	state, ok := shard.instances[key.instance]
	if !ok {
		shard.mu.Unlock()
		c.refreshMetrics()
		return
	}

	var ops []indexOp
	for nodeID, req := range state.current {
		if prev, shared := state.previous[nodeID]; shared && sameWatchTarget(prev, req) {
			continue
		}
		ops = append(ops, indexOp{gvr: req.GVR, add: false, key: key, req: *req, nodeID: req.NodeID})
	}

	state.current = make(map[string]*WatchRequest)
	shard.mu.Unlock()

	c.applyIndexOps(ops)
	c.checkAndStopOrphans(ops)
	c.refreshMetrics()
}

// RemoveInstance removes all watch requests for a specific instance.
// Called when an instance is deleted.
func (c *WatchCoordinator) RemoveInstance(parentGVR schema.GroupVersionResource, instance types.NamespacedName) {
	key := instanceKey{parentGVR: parentGVR, instance: instance}
	shard := c.getParentShard(parentGVR)
	shard.mu.Lock()

	state, ok := shard.instances[key.instance]
	if !ok {
		shard.mu.Unlock()
		c.refreshMetrics()
		return
	}

	var ops []indexOp
	for _, req := range state.current {
		ops = append(ops, indexOp{gvr: req.GVR, add: false, key: key, req: *req, nodeID: req.NodeID})
	}
	for _, req := range state.previous {
		ops = append(ops, indexOp{gvr: req.GVR, add: false, key: key, req: *req, nodeID: req.NodeID})
	}

	delete(shard.instances, key.instance)
	shard.mu.Unlock()

	c.applyIndexOps(ops)
	c.checkAndStopOrphans(ops)
	c.refreshMetrics()
}

// RemoveParentGVR removes all instances for a given parent GVR.
// Called when an RGD is deregistered.
func (c *WatchCoordinator) RemoveParentGVR(parentGVR schema.GroupVersionResource) {
	shard := c.getParentShard(parentGVR)
	shard.mu.Lock()

	var ops []indexOp
	for nn, state := range shard.instances {
		key := instanceKey{parentGVR: parentGVR, instance: nn}
		for _, req := range state.current {
			ops = append(ops, indexOp{gvr: req.GVR, add: false, key: key, req: *req, nodeID: req.NodeID})
		}
		for _, req := range state.previous {
			ops = append(ops, indexOp{gvr: req.GVR, add: false, key: key, req: *req, nodeID: req.NodeID})
		}
		delete(shard.instances, nn)
	}
	shard.mu.Unlock()

	c.applyIndexOps(ops)
	c.checkAndStopOrphans(ops)
	c.refreshMetrics()
}

// checkAndStopOrphans checks if any child GVRs in the ops have become
// orphaned (zero entries) and stops their watches.
func (c *WatchCoordinator) checkAndStopOrphans(ops []indexOp) {
	seen := make(map[schema.GroupVersionResource]struct{})
	for _, op := range ops {
		if !op.add {
			seen[op.gvr] = struct{}{}
		}
	}
	for gvr := range seen {
		idx := c.getIndexShard(gvr)
		idx.mu.RLock()
		empty := len(idx.scalars) == 0 && len(idx.collections) == 0
		idx.mu.RUnlock()
		if empty {
			c.watches.StopWatch(gvr)
			c.log.V(1).Info("Stopped orphaned child watch", "gvr", gvr)
		}
	}
}

// RouteEvent routes a watch event to all matching instances.
// Called by the watch handler for every event. Only locks the specific
// child GVR's index shard, so events for different GVRs never contend.
func (c *WatchCoordinator) RouteEvent(event Event) {
	v, ok := c.indexShards.Load(event.GVR)
	if !ok {
		return
	}
	idx := v.(*indexShard)

	idx.mu.RLock()
	matched := make(map[instanceKey]struct{})

	// Scalar matches (O(1) per name).
	nn := types.NamespacedName{Name: event.Name, Namespace: event.Namespace}
	for _, entry := range idx.scalars[nn] {
		matched[entry.key] = struct{}{}
	}

	// Collection matches (selector scan).
	for _, entry := range idx.collections {
		if entry.namespace != "" && event.Namespace != entry.namespace {
			continue
		}
		if entry.selector.Matches(labels.Set(event.Labels)) {
			matched[entry.key] = struct{}{}
		} else if len(event.OldLabels) > 0 && entry.selector.Matches(labels.Set(event.OldLabels)) {
			matched[entry.key] = struct{}{}
		}
	}
	idx.mu.RUnlock()

	for key := range matched {
		c.enqueue(key.parentGVR, key.instance)
	}
	if len(matched) > 0 {
		c.log.V(2).Info("Routed event", "gvr", event.GVR, "name", event.Name, "namespace", event.Namespace, "type", event.Type)
	}
}

// InstanceWatchCount returns the number of tracked instances.
func (c *WatchCoordinator) InstanceWatchCount() int {
	count := 0
	c.parentShards.Range(func(_, v any) bool {
		shard := v.(*parentShard)
		shard.mu.Lock()
		count += len(shard.instances)
		shard.mu.Unlock()
		return true
	})
	return count
}

func (c *WatchCoordinator) metricSummary() coordinatorMetricSummary {
	summary := coordinatorMetricSummary{
		InstanceWatchCountByParent:   make(map[string]int),
		ScalarWatchRequestsByGVR:     make(map[string]int),
		CollectionWatchRequestsByGVR: make(map[string]int),
	}

	c.parentShards.Range(func(k, v any) bool {
		gvr := k.(schema.GroupVersionResource)
		shard := v.(*parentShard)
		shard.mu.Lock()
		summary.InstanceWatchCountByParent[keyFromGVR(gvr)] = len(shard.instances)
		shard.mu.Unlock()
		return true
	})

	c.indexShards.Range(func(k, v any) bool {
		gvr := k.(schema.GroupVersionResource)
		idx := v.(*indexShard)
		key := keyFromGVR(gvr)
		idx.mu.RLock()
		for _, entries := range idx.scalars {
			summary.ScalarWatchRequestsByGVR[key] += len(entries)
		}
		summary.CollectionWatchRequestsByGVR[key] = len(idx.collections)
		idx.mu.RUnlock()
		return true
	})

	return summary
}

// WatchRequestCount returns the total number of active watch requests.
func (c *WatchCoordinator) WatchRequestCount() (scalar, collection int) {
	c.indexShards.Range(func(_, v any) bool {
		idx := v.(*indexShard)
		idx.mu.RLock()
		for _, entries := range idx.scalars {
			scalar += len(entries)
		}
		collection += len(idx.collections)
		idx.mu.RUnlock()
		return true
	})
	return
}

func (c *WatchCoordinator) refreshMetrics() {
	summary := c.metricSummary()
	syncCoordinatorMetrics(summary, c.watches.ActiveWatchCount())
}

// HasRequestsForGVR reports whether any child or external watch requests still
// exist for the given GVR.
func (c *WatchCoordinator) HasRequestsForGVR(gvr schema.GroupVersionResource) bool {
	v, ok := c.indexShards.Load(gvr)
	if !ok {
		return false
	}
	idx := v.(*indexShard)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.scalars) > 0 || len(idx.collections) > 0
}

// --- index shard helpers (must be called with idx.mu held for write) ---

func addScalarEntry(idx *indexShard, key instanceKey, req WatchRequest) {
	nn := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	for _, entry := range idx.scalars[nn] {
		if entry.key == key && entry.nodeID == req.NodeID {
			return
		}
	}
	idx.scalars[nn] = append(idx.scalars[nn], scalarEntry{
		nodeID: req.NodeID,
		key:    key,
	})
}

func addCollectionEntry(idx *indexShard, key instanceKey, req WatchRequest) {
	for _, e := range idx.collections {
		if e.key == key &&
			e.nodeID == req.NodeID &&
			e.namespace == req.Namespace &&
			e.selector.String() == req.Selector.String() {
			return
		}
	}
	idx.collections = append(idx.collections, collectionEntry{
		nodeID:    req.NodeID,
		selector:  req.Selector,
		namespace: req.Namespace,
		key:       key,
	})
}

func removeScalarEntry(idx *indexShard, key instanceKey, req *WatchRequest) {
	nn := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	entries, ok := idx.scalars[nn]
	if !ok {
		return
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.key == key && entry.nodeID == req.NodeID {
			continue
		}
		filtered = append(filtered, entry)
	}
	if len(filtered) == 0 {
		delete(idx.scalars, nn)
	} else {
		idx.scalars[nn] = filtered
	}
}

func removeCollectionEntry(idx *indexShard, key instanceKey, req *WatchRequest) {
	filtered := idx.collections[:0]
	for _, e := range idx.collections {
		if e.key == key &&
			e.nodeID == req.NodeID &&
			e.namespace == req.Namespace &&
			e.selector.String() == req.Selector.String() {
			continue
		}
		filtered = append(filtered, e)
	}
	if len(filtered) == 0 {
		idx.collections = nil
	} else {
		idx.collections = filtered
	}
}

// stopWatches stops informers for the given GVRs.
func (c *WatchCoordinator) stopWatches(gvrs []schema.GroupVersionResource) {
	for _, gvr := range gvrs {
		c.watches.StopWatch(gvr)
		c.log.V(1).Info("Stopped orphaned child watch", "gvr", gvr)
	}
}

// NoopInstanceWatcher is a no-op implementation of InstanceWatcher for use
// in tests or when no coordinator is available.
type NoopInstanceWatcher struct{}

func (NoopInstanceWatcher) Watch(_ WatchRequest) error { return nil }
func (NoopInstanceWatcher) Done(bool)                  {}

// instanceWatcher is the concrete implementation of InstanceWatcher.
// It buffers Watch() requests locally (zero contention) and flushes them
// to the coordinator in a single lock acquisition when Done(true) is called.
type instanceWatcher struct {
	coordinator *WatchCoordinator
	parentGVR   schema.GroupVersionResource
	instance    types.NamespacedName
	pending     []WatchRequest
}

// Watch buffers a watch request locally. No locks are acquired.
func (w *instanceWatcher) Watch(req WatchRequest) error {
	w.pending = append(w.pending, req)
	return nil
}

// Done finalizes the current reconciliation cycle. If commit is true, all
// buffered requests are flushed to the coordinator in a single lock
// acquisition. If commit is false, the buffer is discarded.
func (w *instanceWatcher) Done(commit bool) {
	key := instanceKey{
		parentGVR: w.parentGVR,
		instance:  w.instance,
	}
	if !commit {
		w.pending = nil
		w.coordinator.abortInstance(key)
		return
	}
	w.coordinator.commitWatches(key, w.pending)
	w.pending = nil
}

func sameWatchTarget(a, b *WatchRequest) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.GVR != b.GVR || a.Name != b.Name || a.Namespace != b.Namespace {
		return false
	}
	if a.isCollection() != b.isCollection() {
		return false
	}
	if !a.isCollection() {
		return true
	}
	return a.Selector.String() == b.Selector.String()
}
