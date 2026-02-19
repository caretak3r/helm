/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sequencing

import (
	"fmt"
	"sort"
	"strings"
)

// DAG is a directed acyclic graph with string-keyed nodes.
// Edges express "must deploy before" relationships: an edge from A→B
// means A must be fully ready before B begins deployment.
type DAG struct {
	nodes map[string]bool
	edges map[string]map[string]bool // from → set-of(to)
}

// NewDAG returns an empty DAG.
func NewDAG() *DAG {
	return &DAG{
		nodes: make(map[string]bool),
		edges: make(map[string]map[string]bool),
	}
}

// AddNode adds a node to the graph. Duplicate adds are no-ops.
func (d *DAG) AddNode(name string) {
	d.nodes[name] = true
}

// AddEdge adds a directed edge from → to. Both nodes are implicitly added.
// Returns an error if from == to (self-loop).
func (d *DAG) AddEdge(from, to string) error {
	if from == to {
		return fmt.Errorf("self-loop detected: %q depends on itself", from)
	}
	d.AddNode(from)
	d.AddNode(to)
	if d.edges[from] == nil {
		d.edges[from] = make(map[string]bool)
	}
	d.edges[from][to] = true
	return nil
}

// Nodes returns the sorted list of node names.
func (d *DAG) Nodes() []string {
	out := make([]string, 0, len(d.nodes))
	for n := range d.nodes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// HasNode returns true if the named node exists.
func (d *DAG) HasNode(name string) bool {
	return d.nodes[name]
}

// Edges returns a copy of the adjacency list.
func (d *DAG) Edges() map[string][]string {
	out := make(map[string][]string, len(d.edges))
	for from, tos := range d.edges {
		for to := range tos {
			out[from] = append(out[from], to)
		}
		sort.Strings(out[from])
	}
	return out
}

// Validate checks for cycles using Kahn's algorithm.
// Returns an error describing the cycle if one is found.
func (d *DAG) Validate() error {
	_, err := d.topologicalSort()
	return err
}

// TopologicalSort returns nodes grouped into batches (levels).
// Nodes in the same batch have no ordering constraints between each other
// and can be deployed concurrently. Batches are returned in deployment order:
// batch[0] has no dependencies, batch[1] depends only on batch[0], etc.
//
// Returns an error if the graph contains a cycle.
func (d *DAG) TopologicalSort() ([][]string, error) {
	return d.topologicalSort()
}

// topologicalSort implements Kahn's algorithm with level tracking.
func (d *DAG) topologicalSort() ([][]string, error) {
	if len(d.nodes) == 0 {
		return nil, nil
	}

	// Build in-degree map
	inDegree := make(map[string]int, len(d.nodes))
	for n := range d.nodes {
		inDegree[n] = 0
	}
	for _, tos := range d.edges {
		for to := range tos {
			inDegree[to]++
		}
	}

	// Seed the queue with zero-degree nodes
	var queue []string
	for n, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, n)
		}
	}
	sort.Strings(queue) // deterministic output

	var batches [][]string
	processed := 0

	for len(queue) > 0 {
		batch := queue
		queue = nil
		sort.Strings(batch)
		batches = append(batches, batch)
		processed += len(batch)

		var next []string
		for _, n := range batch {
			for to := range d.edges[n] {
				inDegree[to]--
				if inDegree[to] == 0 {
					next = append(next, to)
				}
			}
		}
		sort.Strings(next)
		queue = next
	}

	if processed != len(d.nodes) {
		// Remaining nodes form one or more cycles
		var cycleNodes []string
		for n, deg := range inDegree {
			if deg > 0 {
				cycleNodes = append(cycleNodes, n)
			}
		}
		sort.Strings(cycleNodes)
		return nil, fmt.Errorf("dependency cycle detected among: [%s]", strings.Join(cycleNodes, ", "))
	}

	return batches, nil
}

// Reverse returns a new DAG with all edges reversed.
// Useful for computing uninstall order from an install-order DAG.
func (d *DAG) Reverse() *DAG {
	rev := NewDAG()
	for n := range d.nodes {
		rev.AddNode(n)
	}
	for from, tos := range d.edges {
		for to := range tos {
			if rev.edges[to] == nil {
				rev.edges[to] = make(map[string]bool)
			}
			rev.edges[to][from] = true
		}
	}
	return rev
}
