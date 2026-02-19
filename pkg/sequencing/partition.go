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
	"regexp"
	"strings"

	releaseutil "helm.sh/helm/v4/pkg/release/v1/util"
)

// sourceComment matches the "# Source: chartname/..." comment that Helm
// injects into rendered manifests.
var sourceComment = regexp.MustCompile(`(?m)^# Source:\s*(.+)$`)

// PartitionBySubchart splits a rendered manifest string into groups keyed
// by originating subchart name. The parent chart's own resources are keyed
// by parentName. Resources from subchart "foo" nested under parentName
// will be keyed by "foo".
//
// Manifest file paths follow the pattern:
//
//	parentName/templates/foo.yaml           → parentName
//	parentName/charts/subchartName/templates/bar.yaml → subchartName
func PartitionBySubchart(manifest string, parentName string) map[string]string {
	parts := releaseutil.SplitManifests(manifest)
	groups := make(map[string][]string)

	for _, content := range parts {
		name := extractSubchartName(content, parentName)
		groups[name] = append(groups[name], content)
	}

	result := make(map[string]string, len(groups))
	for name, manifests := range groups {
		result[name] = strings.Join(manifests, "\n---\n")
	}
	return result
}

// extractSubchartName extracts the subchart name from a manifest's
// # Source: comment. Returns parentName if the manifest belongs to the
// parent chart directly.
func extractSubchartName(manifest string, parentName string) string {
	matches := sourceComment.FindStringSubmatch(manifest)
	if len(matches) < 2 {
		return parentName
	}
	sourcePath := strings.TrimSpace(matches[1])

	// Strip the parent chart name prefix
	// Path format: parentName/charts/subchartName/templates/foo.yaml
	// or:          parentName/templates/foo.yaml
	parts := strings.Split(sourcePath, "/")
	if len(parts) < 2 {
		return parentName
	}

	// Skip the first segment (parentName) and look for "charts" segment
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "charts" && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return parentName
}
