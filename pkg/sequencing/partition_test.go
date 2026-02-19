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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPartitionBySubchart_Simple(t *testing.T) {
	manifest := `---
# Source: myapp/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp
---
# Source: myapp/charts/db/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: db
---
# Source: myapp/charts/cache/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cache`

	groups := PartitionBySubchart(manifest, "myapp")
	assert.Contains(t, groups, "myapp")
	assert.Contains(t, groups, "db")
	assert.Contains(t, groups, "cache")
	assert.Contains(t, groups["myapp"], "kind: Service")
	assert.Contains(t, groups["db"], "kind: Deployment")
	assert.Contains(t, groups["cache"], "kind: Deployment")
}

func TestPartitionBySubchart_Nested(t *testing.T) {
	manifest := `---
# Source: parent/charts/sub/charts/subsub/templates/cm.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: subsub-config`

	groups := PartitionBySubchart(manifest, "parent")
	// First charts/ segment yields "sub"
	assert.Contains(t, groups, "sub")
}

func TestPartitionBySubchart_NoSource(t *testing.T) {
	manifest := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: orphan`

	groups := PartitionBySubchart(manifest, "myapp")
	assert.Contains(t, groups, "myapp")
}

func TestPartitionBySubchart_Empty(t *testing.T) {
	groups := PartitionBySubchart("", "myapp")
	assert.Empty(t, groups)
}

func TestPartitionBySubchart_SingleChart(t *testing.T) {
	manifest := `---
# Source: myapp/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp`

	groups := PartitionBySubchart(manifest, "myapp")
	assert.Len(t, groups, 1)
	assert.Contains(t, groups, "myapp")
}

func TestExtractSubchartName(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		parent   string
		expected string
	}{
		{
			name:     "parent template",
			manifest: "# Source: myapp/templates/svc.yaml\nkind: Service",
			parent:   "myapp",
			expected: "myapp",
		},
		{
			name:     "subchart template",
			manifest: "# Source: myapp/charts/db/templates/deploy.yaml\nkind: Deployment",
			parent:   "myapp",
			expected: "db",
		},
		{
			name:     "nested subchart",
			manifest: "# Source: myapp/charts/sub/charts/inner/templates/cm.yaml\nkind: ConfigMap",
			parent:   "myapp",
			expected: "sub",
		},
		{
			name:     "no source comment",
			manifest: "kind: ConfigMap",
			parent:   "myapp",
			expected: "myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSubchartName(tt.manifest, tt.parent)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// Post-renderer edge case tests

func TestExtractSubchartName_MangledSourceComment(t *testing.T) {
	// Post-renderer might add extra whitespace around source comment
	got := extractSubchartName("# Source:   myapp/charts/db/templates/deploy.yaml  \nkind: Deployment", "myapp")
	assert.Equal(t, "db", got)
}

func TestExtractSubchartName_ShortPath(t *testing.T) {
	// Path with only one segment (unusual but shouldn't panic)
	got := extractSubchartName("# Source: single\nkind: ConfigMap", "myapp")
	assert.Equal(t, "myapp", got)
}

func TestExtractSubchartName_EmptyManifest(t *testing.T) {
	got := extractSubchartName("", "myapp")
	assert.Equal(t, "myapp", got)
}

func TestPartitionBySubchart_PostRendererModified(t *testing.T) {
	// Simulate post-renderer that removes Source comments from some manifests
	manifest := `---
# Source: myapp/charts/db/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: db
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: injected-by-post-renderer`

	groups := PartitionBySubchart(manifest, "myapp")
	assert.Contains(t, groups, "db")
	// Manifest without Source comment falls back to parent
	assert.Contains(t, groups, "myapp")
	assert.Contains(t, groups["myapp"], "injected-by-post-renderer")
}

func TestPartitionBySubchart_MultipleManifestsPerSubchart(t *testing.T) {
	manifest := `---
# Source: myapp/charts/db/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: db-deploy
---
# Source: myapp/charts/db/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: db-svc`

	groups := PartitionBySubchart(manifest, "myapp")
	assert.Contains(t, groups, "db")
	assert.Contains(t, groups["db"], "db-deploy")
	assert.Contains(t, groups["db"], "db-svc")
}

// Benchmarks

func BenchmarkDAGConstruction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		d := NewDAG()
		// Linear chain of 100 nodes
		for j := 0; j < 100; j++ {
			d.AddNode(fmt.Sprintf("node-%d", j))
		}
		for j := 0; j < 99; j++ {
			d.AddEdge(fmt.Sprintf("node-%d", j), fmt.Sprintf("node-%d", j+1))
		}
	}
}

func BenchmarkTopologicalSort_Linear(b *testing.B) {
	d := NewDAG()
	for j := 0; j < 100; j++ {
		d.AddNode(fmt.Sprintf("node-%d", j))
	}
	for j := 0; j < 99; j++ {
		d.AddEdge(fmt.Sprintf("node-%d", j), fmt.Sprintf("node-%d", j+1))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.TopologicalSort()
	}
}

func BenchmarkTopologicalSort_Diamond(b *testing.B) {
	d := NewDAG()
	// Create a wide diamond: root → 50 middle nodes → leaf
	d.AddNode("root")
	d.AddNode("leaf")
	for j := 0; j < 50; j++ {
		name := fmt.Sprintf("mid-%d", j)
		d.AddNode(name)
		d.AddEdge("root", name)
		d.AddEdge(name, "leaf")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.TopologicalSort()
	}
}

func BenchmarkPartitionBySubchart(b *testing.B) {
	// Build a realistic manifest with 20 subcharts, 3 resources each
	var builder strings.Builder
	for i := 0; i < 20; i++ {
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&builder, "---\n# Source: myapp/charts/sub%d/templates/res%d.yaml\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sub%d-res%d\n", i, j, i, j)
		}
	}
	manifest := builder.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PartitionBySubchart(manifest, "myapp")
	}
}
