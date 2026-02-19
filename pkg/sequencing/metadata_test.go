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
	"testing"
)

func TestSequencingMetadataMarshalRoundTrip(t *testing.T) {
	meta := &SequencingMetadata{
		SubchartOrder: [][]string{{"db"}, {"cache"}, {"web"}, {"parent"}},
		Dependencies: map[string][]string{
			"cache":  {"db"},
			"web":    {"cache"},
			"parent": {"db", "cache", "web"},
		},
	}

	raw, err := meta.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	got, err := UnmarshalSequencingMetadata(raw)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(got.SubchartOrder) != 4 {
		t.Errorf("expected 4 batches, got %d", len(got.SubchartOrder))
	}
	if got.SubchartOrder[0][0] != "db" {
		t.Errorf("expected first batch = [db], got %v", got.SubchartOrder[0])
	}
	if len(got.Dependencies["cache"]) != 1 || got.Dependencies["cache"][0] != "db" {
		t.Errorf("expected cache deps = [db], got %v", got.Dependencies["cache"])
	}
}

func TestUnmarshalSequencingMetadataNil(t *testing.T) {
	meta, err := UnmarshalSequencingMetadata(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Fatalf("expected nil metadata, got %+v", meta)
	}
}

func TestUnmarshalSequencingMetadataEmpty(t *testing.T) {
	meta, err := UnmarshalSequencingMetadata(json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Fatalf("expected nil metadata, got %+v", meta)
	}
}

func TestUnmarshalSequencingMetadataInvalid(t *testing.T) {
	_, err := UnmarshalSequencingMetadata(json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReconstructDAG(t *testing.T) {
	meta := &SequencingMetadata{
		SubchartOrder: [][]string{{"db"}, {"cache"}, {"web"}, {"parent"}},
		Dependencies: map[string][]string{
			"cache":  {"db"},
			"web":    {"cache"},
			"parent": {"db", "cache", "web"},
		},
	}

	dag, err := ReconstructDAG(meta)
	if err != nil {
		t.Fatalf("ReconstructDAG failed: %v", err)
	}

	levels, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	// db must be in first level
	if len(levels) < 2 {
		t.Fatalf("expected at least 2 levels, got %d", len(levels))
	}
	if levels[0][0] != "db" {
		t.Errorf("expected first level = [db], got %v", levels[0])
	}
}

func TestReconstructDAGNil(t *testing.T) {
	dag, err := ReconstructDAG(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag != nil {
		t.Fatalf("expected nil DAG, got %+v", dag)
	}
}

func TestReconstructDAGReverse(t *testing.T) {
	meta := &SequencingMetadata{
		SubchartOrder: [][]string{{"a"}, {"b"}, {"c"}},
		Dependencies: map[string][]string{
			"b": {"a"},
			"c": {"b"},
		},
	}

	dag, err := ReconstructDAG(meta)
	if err != nil {
		t.Fatalf("ReconstructDAG failed: %v", err)
	}

	reversed := dag.Reverse()
	levels, err := reversed.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort on reversed DAG failed: %v", err)
	}

	// In reversed DAG, c should come first (no incoming edges)
	if levels[0][0] != "c" {
		t.Errorf("expected reversed first level = [c], got %v", levels[0])
	}
}
