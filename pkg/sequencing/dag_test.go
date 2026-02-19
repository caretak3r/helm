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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDAG_Empty(t *testing.T) {
	d := NewDAG()
	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	assert.Nil(t, batches)
}

func TestDAG_SingleNode(t *testing.T) {
	d := NewDAG()
	d.AddNode("a")
	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"a"}}, batches)
}

func TestDAG_LinearChain(t *testing.T) {
	// A → B → C  (A deploys first, then B, then C)
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("B", "C"))

	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"A"}, {"B"}, {"C"}}, batches)
}

func TestDAG_Diamond(t *testing.T) {
	// A → B, A → C, B → D, C → D
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("A", "C"))
	require.NoError(t, d.AddEdge("B", "D"))
	require.NoError(t, d.AddEdge("C", "D"))

	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, batches, 3)
	assert.Equal(t, []string{"A"}, batches[0])
	assert.Equal(t, []string{"B", "C"}, batches[1]) // concurrent
	assert.Equal(t, []string{"D"}, batches[2])
}

func TestDAG_Disconnected(t *testing.T) {
	d := NewDAG()
	d.AddNode("X")
	d.AddNode("Y")
	d.AddNode("Z")

	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, []string{"X", "Y", "Z"}, batches[0]) // all concurrent
}

func TestDAG_CycleDetection(t *testing.T) {
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("B", "C"))
	require.NoError(t, d.AddEdge("C", "A"))

	_, err := d.TopologicalSort()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependency cycle detected")
	assert.Contains(t, err.Error(), "A")
	assert.Contains(t, err.Error(), "B")
	assert.Contains(t, err.Error(), "C")
}

func TestDAG_SelfLoop(t *testing.T) {
	d := NewDAG()
	err := d.AddEdge("A", "A")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestDAG_Validate(t *testing.T) {
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	assert.NoError(t, d.Validate())

	d2 := NewDAG()
	require.NoError(t, d2.AddEdge("A", "B"))
	require.NoError(t, d2.AddEdge("B", "A"))
	assert.Error(t, d2.Validate())
}

func TestDAG_Reverse(t *testing.T) {
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("B", "C"))

	rev := d.Reverse()
	batches, err := rev.TopologicalSort()
	require.NoError(t, err)
	// Reversed: C first, then B, then A
	assert.Equal(t, [][]string{{"C"}, {"B"}, {"A"}}, batches)
}

func TestDAG_ReverseDiamond(t *testing.T) {
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("A", "C"))
	require.NoError(t, d.AddEdge("B", "D"))
	require.NoError(t, d.AddEdge("C", "D"))

	rev := d.Reverse()
	batches, err := rev.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, batches, 3)
	assert.Equal(t, []string{"D"}, batches[0])
	assert.Equal(t, []string{"B", "C"}, batches[1])
	assert.Equal(t, []string{"A"}, batches[2])
}

func TestDAG_HasNode(t *testing.T) {
	d := NewDAG()
	d.AddNode("X")
	assert.True(t, d.HasNode("X"))
	assert.False(t, d.HasNode("Y"))
}

func TestDAG_Nodes(t *testing.T) {
	d := NewDAG()
	d.AddNode("C")
	d.AddNode("A")
	d.AddNode("B")
	assert.Equal(t, []string{"A", "B", "C"}, d.Nodes())
}

func TestDAG_Edges(t *testing.T) {
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("A", "C"))
	edges := d.Edges()
	assert.Equal(t, []string{"B", "C"}, edges["A"])
}

func TestDAG_DuplicateEdge(t *testing.T) {
	d := NewDAG()
	require.NoError(t, d.AddEdge("A", "B"))
	require.NoError(t, d.AddEdge("A", "B")) // duplicate, should be no-op
	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"A"}, {"B"}}, batches)
}

func TestDAG_ComplexGraph(t *testing.T) {
	// db → backend → frontend, db → cache → backend
	d := NewDAG()
	require.NoError(t, d.AddEdge("db", "backend"))
	require.NoError(t, d.AddEdge("db", "cache"))
	require.NoError(t, d.AddEdge("cache", "backend"))
	require.NoError(t, d.AddEdge("backend", "frontend"))

	batches, err := d.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"db"}, {"cache"}, {"backend"}, {"frontend"}}, batches)
}
