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

const (
	// SubchartDependsOnAnnotation is the annotation key used to declare
	// subchart deployment ordering dependencies on a chart's metadata.
	// Value is a comma-separated list of subchart names (or a JSON array).
	SubchartDependsOnAnnotation = "helm.sh/depends-on/subcharts"

	// ResourceGroupAnnotation is the annotation key used to assign a
	// resource to a named group for intra-chart sequencing.
	ResourceGroupAnnotation = "helm.sh/resource-group"

	// ResourceGroupDependsOnAnnotation is the annotation key used to
	// declare ordering between resource groups within a single chart.
	ResourceGroupDependsOnAnnotation = "helm.sh/depends-on/resource-groups"
)
