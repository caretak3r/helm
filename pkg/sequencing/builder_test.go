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

	v3chart "helm.sh/helm/v4/internal/chart/v3"
	"helm.sh/helm/v4/pkg/chart/common"
)

func makeTestChart(name string, deps []*v3chart.Dependency, annotations map[string]string) *v3chart.Chart {
	ch := &v3chart.Chart{
		Metadata: &v3chart.Metadata{
			APIVersion:   "v3",
			Name:         name,
			Version:      "1.0.0",
			Dependencies: deps,
			Annotations:  annotations,
		},
		Templates: []*common.File{},
	}
	// Add loaded subchart objects so that Dependencies() length matches
	// Metadata.Dependencies — the v3Accessor.MetaDependencies() requires this.
	for _, dep := range deps {
		depName := dep.Name
		if dep.Alias != "" {
			depName = dep.Alias
		}
		sub := &v3chart.Chart{
			Metadata: &v3chart.Metadata{
				APIVersion: "v3",
				Name:       depName,
				Version:    dep.Version,
			},
		}
		ch.AddDependency(sub)
	}
	return ch
}

func TestBuildSubchartDAG_NoDeps(t *testing.T) {
	chrt := makeTestChart("parent", nil, nil)
	dag, err := BuildSubchartDAG(chrt)
	require.NoError(t, err)
	batches, err := dag.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"parent"}}, batches)
}

func TestBuildSubchartDAG_LinearChain(t *testing.T) {
	// A → B → C, all subcharts of parent
	deps := []*v3chart.Dependency{
		{Name: "A", Version: "1.0.0", Repository: "https://example.com"},
		{Name: "B", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"A"}},
		{Name: "C", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"B"}},
	}
	chrt := makeTestChart("parent", deps, nil)
	dag, err := BuildSubchartDAG(chrt)
	require.NoError(t, err)

	batches, err := dag.TopologicalSort()
	require.NoError(t, err)
	// A first, then B, then C, then parent (parent last)
	assert.Equal(t, [][]string{{"A"}, {"B"}, {"C"}, {"parent"}}, batches)
}

func TestBuildSubchartDAG_Diamond(t *testing.T) {
	deps := []*v3chart.Dependency{
		{Name: "db", Version: "1.0.0", Repository: "https://example.com"},
		{Name: "cache", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"db"}},
		{Name: "api", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"db"}},
		{Name: "web", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"cache", "api"}},
	}
	chrt := makeTestChart("myapp", deps, nil)
	dag, err := BuildSubchartDAG(chrt)
	require.NoError(t, err)

	batches, err := dag.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, batches, 4)
	assert.Equal(t, []string{"db"}, batches[0])
	assert.Equal(t, []string{"api", "cache"}, batches[1])
	assert.Equal(t, []string{"web"}, batches[2])
	assert.Equal(t, []string{"myapp"}, batches[3])
}

func TestBuildSubchartDAG_Cycle(t *testing.T) {
	deps := []*v3chart.Dependency{
		{Name: "A", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"B"}},
		{Name: "B", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"A"}},
	}
	chrt := makeTestChart("parent", deps, nil)
	_, err := BuildSubchartDAG(chrt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestBuildSubchartDAG_WithAlias(t *testing.T) {
	deps := []*v3chart.Dependency{
		{Name: "postgresql", Alias: "db", Version: "1.0.0", Repository: "https://example.com"},
		{Name: "redis", Alias: "cache", Version: "1.0.0", Repository: "https://example.com", DependsOn: []string{"db"}},
	}
	chrt := makeTestChart("parent", deps, nil)
	dag, err := BuildSubchartDAG(chrt)
	require.NoError(t, err)

	// Aliases used as node names
	assert.True(t, dag.HasNode("db"))
	assert.True(t, dag.HasNode("cache"))
	assert.False(t, dag.HasNode("postgresql"))
}

func TestBuildSubchartDAG_AnnotationOnly(t *testing.T) {
	deps := []*v3chart.Dependency{
		{Name: "A", Version: "1.0.0", Repository: "https://example.com"},
		{Name: "B", Version: "1.0.0", Repository: "https://example.com"},
	}
	annotations := map[string]string{
		SubchartDependsOnAnnotation: "A:B",
	}
	chrt := makeTestChart("parent", deps, annotations)
	dag, err := BuildSubchartDAG(chrt)
	require.NoError(t, err)

	batches, err := dag.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"A"}, {"B"}, {"parent"}}, batches)
}

func TestBuildSubchartDAG_AnnotationJSON(t *testing.T) {
	deps := []*v3chart.Dependency{
		{Name: "A", Version: "1.0.0", Repository: "https://example.com"},
		{Name: "B", Version: "1.0.0", Repository: "https://example.com"},
	}
	annotations := map[string]string{
		SubchartDependsOnAnnotation: `[{"name":"B","depends-on":["A"]}]`,
	}
	chrt := makeTestChart("parent", deps, annotations)
	dag, err := BuildSubchartDAG(chrt)
	require.NoError(t, err)

	batches, err := dag.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"A"}, {"B"}, {"parent"}}, batches)
}

func TestParseAnnotationList_Pairs(t *testing.T) {
	pairs, err := parseAnnotationList("A:B, C:D")
	require.NoError(t, err)
	assert.Equal(t, [][2]string{{"A", "B"}, {"C", "D"}}, pairs)
}

func TestParseAnnotationList_JSON(t *testing.T) {
	pairs, err := parseAnnotationList(`[{"name":"B","depends-on":["A","C"]}]`)
	require.NoError(t, err)
	assert.Equal(t, [][2]string{{"A", "B"}, {"C", "B"}}, pairs)
}

func TestParseAnnotationList_Empty(t *testing.T) {
	pairs, err := parseAnnotationList("")
	require.NoError(t, err)
	assert.Nil(t, pairs)
}

func TestParseAnnotationList_Invalid(t *testing.T) {
	_, err := parseAnnotationList("nocolon")
	require.Error(t, err)
}
