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

import "encoding/json"

// SequencingMetadata records the deployment order used when --wait=ordered was
// active. Stored in the Release as json.RawMessage to avoid coupling
// pkg/release/v1 to pkg/sequencing.
type SequencingMetadata struct {
	// SubchartOrder contains subchart names grouped by deployment batch.
	// batch[0] was deployed first, batch[len-1] last (typically the parent).
	SubchartOrder [][]string `json:"subchartOrder,omitempty"`

	// Dependencies maps each subchart name to its direct dependency names
	// (the subcharts it depends on). Used to reconstruct the DAG for rollback.
	Dependencies map[string][]string `json:"dependencies,omitempty"`
}

// Marshal serializes SequencingMetadata to json.RawMessage for storage.
func (m *SequencingMetadata) Marshal() (json.RawMessage, error) {
	return json.Marshal(m)
}

// UnmarshalSequencingMetadata deserializes a json.RawMessage into
// SequencingMetadata. Returns nil metadata and nil error if raw is empty/nil.
func UnmarshalSequencingMetadata(raw json.RawMessage) (*SequencingMetadata, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m SequencingMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ReconstructDAG rebuilds a DAG from stored SequencingMetadata.
// Used during rollback to restore the original deployment order.
func ReconstructDAG(meta *SequencingMetadata) (*DAG, error) {
	if meta == nil {
		return nil, nil
	}
	dag := NewDAG()

	// Add all nodes from the stored order
	for _, batch := range meta.SubchartOrder {
		for _, name := range batch {
			dag.AddNode(name)
		}
	}

	// Reconstruct edges from the dependency map
	for name, deps := range meta.Dependencies {
		for _, dep := range deps {
			if err := dag.AddEdge(dep, name); err != nil {
				return nil, err
			}
		}
	}

	return dag, nil
}
