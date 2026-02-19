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
	"encoding/json"
	"fmt"
	"strings"

	"helm.sh/helm/v4/pkg/chart"
)

// BuildSubchartDAG constructs a DAG representing subchart deployment order
// from a chart's metadata. It reads DependsOn fields from each dependency
// as well as the helm.sh/depends-on/subcharts annotation on the chart metadata.
//
// The parent chart itself is added as a node. Per HIP-0025, the parent chart's
// own resources deploy last unless explicitly ordered otherwise.
func BuildSubchartDAG(chrt chart.Charter) (*DAG, error) {
	accessor, err := chart.NewAccessor(chrt)
	if err != nil {
		return nil, fmt.Errorf("unable to access chart metadata: %w", err)
	}

	dag := NewDAG()
	parentName := accessor.Name()
	dag.AddNode(parentName)

	// Build a lookup of dependency name/alias → effective name
	deps := accessor.MetaDependencies()
	depNames := make(map[string]bool, len(deps))

	for _, dep := range deps {
		dac, err := chart.NewDependencyAccessor(dep)
		if err != nil {
			return nil, fmt.Errorf("unable to access dependency: %w", err)
		}
		name := dac.Name()
		if alias := dac.Alias(); alias != "" {
			name = alias
		}
		depNames[name] = true
		dag.AddNode(name)

		// Process DependsOn field from Chart.yaml dependency entries
		for _, ref := range dac.DependsOn() {
			// Edge: ref → name (ref must deploy before name)
			if err := dag.AddEdge(ref, name); err != nil {
				return nil, fmt.Errorf("invalid depends-on for subchart %q: %w", name, err)
			}
		}
	}

	// Process helm.sh/depends-on/subcharts annotation on chart metadata.
	// MetadataAsMap returns Annotations as map[string]string (from the struct field).
	metadata := accessor.MetadataAsMap()
	if ann := metadata["Annotations"]; ann != nil {
		var annotationVal string
		switch a := ann.(type) {
		case map[string]string:
			annotationVal = a[SubchartDependsOnAnnotation]
		case map[string]interface{}:
			if v, ok := a[SubchartDependsOnAnnotation]; ok {
				annotationVal, _ = v.(string)
			}
		}
		if annotationVal != "" {
			refs, err := parseAnnotationList(annotationVal)
			if err != nil {
				return nil, fmt.Errorf("invalid %s annotation: %w", SubchartDependsOnAnnotation, err)
			}
			for _, pair := range refs {
				if err := dag.AddEdge(pair[0], pair[1]); err != nil {
					return nil, fmt.Errorf("invalid annotation depends-on: %w", err)
				}
			}
		}
	}

	// Add edges so parent deploys after all subcharts (HIP-0025 default)
	for name := range depNames {
		if err := dag.AddEdge(name, parentName); err != nil {
			return nil, fmt.Errorf("unable to add parent dependency edge: %w", err)
		}
	}

	if err := dag.Validate(); err != nil {
		return nil, err
	}

	return dag, nil
}

// parseAnnotationList parses the helm.sh/depends-on/subcharts annotation value.
// Supported formats:
//   - Comma-separated pairs: "A:B,C:D" (A before B, C before D)
//   - JSON array of objects: [{"name":"B","depends-on":["A"]}]
func parseAnnotationList(s string) ([][2]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	// Try JSON array format first
	if strings.HasPrefix(s, "[") {
		var entries []struct {
			Name      string   `json:"name"`
			DependsOn []string `json:"depends-on"`
		}
		if err := json.Unmarshal([]byte(s), &entries); err == nil {
			var pairs [][2]string
			for _, e := range entries {
				for _, dep := range e.DependsOn {
					pairs = append(pairs, [2]string{dep, e.Name})
				}
			}
			return pairs, nil
		}
	}

	// Comma-separated pairs: "dep:name,dep2:name2"
	var pairs [][2]string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sides := strings.SplitN(part, ":", 2)
		if len(sides) != 2 {
			return nil, fmt.Errorf("expected 'from:to' pair, got %q", part)
		}
		from := strings.TrimSpace(sides[0])
		to := strings.TrimSpace(sides[1])
		if from == "" || to == "" {
			return nil, fmt.Errorf("empty name in pair %q", part)
		}
		pairs = append(pairs, [2]string{from, to})
	}
	return pairs, nil
}
