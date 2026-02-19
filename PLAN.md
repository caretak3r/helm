# Sequenced subchart handling support

## Overview

This plan outlines the implementation of adding subchart sequencing as described in [HIP-0025](https://github.com/helm/community/blob/main/hips/hip-0025.md). This feature enables charts with subcharts to be installed, upgraded, or uninstalled in a specific order based on dependency definitions, ensuring each subchart is fully deployed and ready before its dependents are processed.

## Prior art / background

- [HIP-0025](https://github.com/helm/community/blob/main/hips/hip-0025.md) - Main specification
- [Project plan gist](https://gist.github.com/banjoh/a8a5598ed0e65494017afc36fc5ad35d) - Detailed implementation plan

## Requirements

### Core Features
- Add new `--wait=ordered` CLI option and corresponding `WaitStrategy` to enable sequencing
- Extend Chart.yaml dependency structure with sequencing fields
- Implement Directed Acyclic Graph (DAG) for dependency resolution
- Store sequencing configuration in release objects
- Maintain backward compatibility with existing charts

### Dependency Declaration Methods
1. **Annotation-based**: `helm.sh/depends-on/subcharts` annotation in Chart.yaml
2. **Dependency field**: `depends-on` field in Chart.yaml dependencies list

### Readiness Evaluation
- Use existing `--wait` flag implementation for subchart readiness determination
- No custom readiness evaluation at this stage

### CLI Options
- `--wait=ordered` - Enable ordered subchart processing
- Use existing `--timeout` flag for subchart readiness timeout

## Implementation details

### Manifest Processing Flow

#### Current Behavior
Today, Helm processes all subcharts and their manifests together:
1. Render all subchart templates simultaneously
2. Concatenate all manifests together
3. Send all manifests to Kubernetes client in a single batch
4. Wait for all resources to be ready (if `--wait` is used)

#### New Behavior with `--wait=ordered`
With subchart sequencing enabled, manifests will be sent in batches based on DAG ordering:

1. **DAG Resolution**: Build dependency graph from subchart `depends-on` relationships
2. **Batch Creation**: Group subcharts by dependency level (topological layers)
3. **Sequential Batch Processing**:
   Given this example chart:
   ```yaml
   name: foo
   annotations:
     helm.sh/depends-on/subcharts: ["bar", "rabbitmq"]
   dependencies:
     - name: nginx
     - name: rabbitmq
     - name: bar
       depends-on: ["nginx", "rabbitmq"]
    - name: orphaned
   ```

   Installation order:
   ```
   Batch 1: [nginx, rabbitmq] (bar depends on these)
   Batch 2: [bar] (depends on nginx, rabbitmq)
   Batch 3: [orphaned, foo] (orphaned has no dependencies, installed with parent)
   ```
4. **Batch Installation Flow**:
   ```go
   for batchIndex, batch := range batches {
       // Render manifests for all subcharts in current batch
       for _, subchart := range batch {
           manifests := renderSubchartManifests(subchart)
           batchManifests = append(batchManifests, manifests...)
       }

       // Send batch manifests to Kubernetes client
       err := i.cfg.KubeClient.Create(batchManifests)

       // Wait for all resources in batch to be ready
       if i.Wait {
           err := i.cfg.KubeClient.WaitForReadiness(batchManifests, i.Timeout)
       }

       // Collect manifests for final storage
       allManifests = append(allManifests, batchManifests...)
   }
   ```
5. **Final Storage**: Concatenate all manifests in installation order and store in `release.Manifest`

#### Key Implementation Points
- **Parallel within batch**: Subcharts at same dependency level install concurrently
- **Sequential between batches**: Next batch waits for previous batch readiness
- **Existing wait logic**: Reuse current `--wait` implementation for readiness checks
- **Backward compatibility**: Without `--wait=ordered`, behavior remains unchanged
- **Nested subchart behavior**: Each chart processes its own DAG for direct dependencies only. When a subchart has nested dependencies, it recursively processes its own DAG first, maintaining atomic unit behavior while avoiding annotation conflicts between chart levels

#### Pseudo Code for New Flow

```go
// In pkg/action/install.go
func (i *Install) installWithSequencing(chart *chart.Chart) error {
    if i.WaitStrategy != OrderedWaitStrategy {
        // Existing behavior - install all at once
        return i.installTraditional(chart)
    }

    return i.installChartWithDAG(chart, i.Wait, i.Timeout)
}

// Hierarchical DAG processing for nested subcharts
func (i *Install) installChartWithDAG(chart *chart.Chart, wait bool, timeout time.Duration) error {
    // Build DAG for this chart's direct dependencies only
    dag := buildHierarchicalDAG(chart)
    batches := dag.GetInstallationBatches()
    
    var allManifests []string
    
    // Process each batch sequentially
    for batchIndex, batch := range batches {
        var batchManifests []string
        
        // Process each subchart in current batch
        for _, subchart := range batch {
            // Recursively process this subchart and its dependencies
            subchartManifests := i.processSubchartRecursively(subchart, wait, timeout)
            batchManifests = append(batchManifests, subchartManifests...)
        }
        
        // Install this batch
        if len(batchManifests) > 0 {
            err := i.cfg.KubeClient.Create(batchManifests)
            if err != nil {
                return fmt.Errorf("failed to install batch %d: %w", batchIndex, err)
            }
            
            // Wait for all resources in batch to be ready
            if wait {
                err := i.cfg.KubeClient.WaitForReadiness(batchManifests, timeout)
                if err != nil {
                    return fmt.Errorf("batch %d failed to become ready: %w", batchIndex, err)
                }
            }
        }
        
        allManifests = append(allManifests, batchManifests...)
    }
    
    // Add parent chart's own resources after all dependencies
    parentManifests := renderChartOwnManifests(chart)
    allManifests = append(allManifests, parentManifests...)
    
    // Store concatenated manifests in topological order
    i.release.Manifest = strings.Join(allManifests, "\n---\n")
    
    return nil
}

func (i *Install) processSubchartRecursively(subchart *chart.Chart, wait bool, timeout time.Duration) []string {
    // If subchart has its own dependencies, process them first
    if hasSubchartDependencies(subchart) {
        // Recursively handle nested subcharts with their own DAG
        return i.installChartWithDAG(subchart, wait, timeout)
    } else {
        // Simple subchart - just render manifests
        return renderSubchartManifests(subchart)
    }
}

// Helper functions
func buildHierarchicalDAG(chart *chart.Chart) *SubchartDAG {
    dag := &SubchartDAG{}
    
    // Add all direct subcharts to DAG
    for _, subchart := range chart.Dependencies() {
        dag.AddNode(subchart)
    }
    
    // Add edges based on depends-on relationships within THIS chart's scope
    for _, subchart := range chart.Dependencies() {
        dependsOn := getDependsOnList(subchart, chart) // Parse from chart's annotations/fields
        for _, depName := range dependsOn {
            depChart := findSubchartByName(chart, depName)
            if depChart != nil {
                dag.AddEdge(depChart, subchart) // depChart must install before subchart
            }
        }
    }
    
    return dag
}

func (dag *SubchartDAG) GetInstallationBatches() [][]Subchart {
    // Perform topological sort
    // Group subcharts by dependency level
    // Return batches for sequential installation
}
```

### Architecture Changes

1. **Chart Metadata Extension** (`pkg/chart/v2/`)
   - ✅ Add `DependsOn []string` field to `Dependency` struct
   - Add annotation parsing for `helm.sh/depends-on/subcharts`

2. **Dependency Processing** (`pkg/chart/v2/util/dependencies.go`)
   - Implement DAG construction and validation
   - Add topological sorting for dependency order
   - Add circular dependency detection

3. **Action System** (`pkg/action/`)
   - Modify install/upgrade actions to support ordered processing
   - Add subchart installation state tracking
   - Implement subchart-specific waiting logic

4. **Wait Strategy Extension** (`pkg/kube/`)
   - Add `OrderedWaitStrategy` for subchart sequencing
   - Reuse existing wait implementation for subchart readiness
   - Add timeout mechanisms using existing timeout handling

5. **Release Storage** (`pkg/release/v1/`)
   - Store DAG reconstruction metadata in release object
   - Implement manifest-based DAG reconstruction for rollbacks
   - Handle post-renderer scenarios with robust subchart identification

6. **Manifest Processing** (`pkg/release/util/`)
   - Add subchart identification from Source comments
   - Handle nested subchart path parsing correctly
   - Support post-renderer edge cases with fallback strategies

7. **Hook System** (`pkg/release/v1/hook.go`)
   - No changes required - use existing hook system

### Key Data Structures

```go
// Enhanced Dependency struct
type Dependency struct {
    Name         string   `json:"name"`
    Version      string   `json:"version,omitempty"`
    Repository   string   `json:"repository,omitempty"`
    DependsOn    []string `json:"dependsOn,omitempty"`  // ✅ IMPLEMENTED
    // ... existing fields
}

// Enhanced Release struct for DAG reconstruction
type Release struct {
    // ... existing fields
    Manifest        string                 `json:"manifest,omitempty"`
    SequencingInfo  *SequencingMetadata    `json:"sequencing,omitempty"`  // NEW
}

// DAG reconstruction metadata
type SequencingMetadata struct {
    Enabled         bool                   `json:"enabled"`
    Strategy        string                 `json:"strategy"`        // "ordered"
    Batches         []BatchInfo            `json:"batches"`
    Dependencies    map[string][]string    `json:"dependencies"`    // subchart -> dependsOn
}

type BatchInfo struct {
    Order          int                     `json:"order"`
    Subcharts      []string                `json:"subcharts"`
}

// Subchart path parsing for manifest analysis
type SubchartPath struct {
    Hierarchy []string  // ["redis", "sentinel"] for nested subcharts
    Immediate string    // "sentinel" - the actual subchart
    Parent    string    // "redis" - parent subchart (if nested)
}

// New wait strategy
const OrderedWaitStrategy WaitStrategy = "ordered"

// Subchart installation state
type SubchartState struct {
    Name      string
    Status    string  // pending, installing, ready, failed
    StartTime time.Time
    EndTime   time.Time
    Error     error
}
```

## Implementation steps

### Phase 1: Core Infrastructure ✅
1. ✅ Analyze existing codebase structure
2. ✅ Review HIP-0025 requirements and design
3. ✅ Create comprehensive implementation plan

### Phase 2: Chart Metadata Extension ✅
4. ✅ Extend `Dependency` struct with `DependsOn` field in `internal/chart/v3/dependency.go`
5. ✅ Add annotation parsing for `helm.sh/depends-on/subcharts` in Chart.yaml processing
6. ✅ Update chart validation to check for circular dependencies
7. ✅ Add unit tests for dependency parsing and validation

### Phase 3: Dependency Graph Construction ✅
8. ✅ Implement DAG construction in `pkg/sequencing/builder.go`
9. ✅ Add topological sorting algorithm (Kahn's) for dependency ordering
10. ✅ Implement circular dependency detection with clear error messages
11. Skipped: dependency resolution caching (low priority, benchmarks show <30µs)
12. ✅ Add unit tests for DAG construction and sorting

### Phase 4: Wait Strategy Extension ✅
13. ✅ Add `OrderedStrategy` constant to `pkg/kube/client.go`
14. ✅ Implement ordered wait strategy reusing existing StatusWatcher
15. ✅ Add timeout mechanisms using existing timeout handling
16. ✅ Add unit tests for wait strategy functionality

### Phase 5: Action System Integration ✅
17. ✅ Modify `Install` action in `pkg/action/install.go` to support ordered processing
18. ✅ Modify `Upgrade` action in `pkg/action/upgrade.go` to support ordered processing
19. ✅ Add subchart installation state tracking (via batch processing)
20. ✅ Implement progress reporting (debug logging per batch)
21. ✅ Add error handling and rollback logic for failed subchart installations
22. ✅ Add integration tests for install/upgrade with sequencing
23. ✅ **Bonus**: Ordered uninstall (`uninstall.go`) — reverse DAG teardown
24. ✅ **Bonus**: Ordered template output (`template.go`) — batch-grouped `helm template`

### Phase 6: CLI Integration ✅
25. ✅ Add `--wait=ordered` flag to install command (`pkg/cmd/flags.go`)
26. ✅ Add `--wait=ordered` flag to upgrade command (shared via `AddWaitFlag`)
27. ✅ Update command help text and documentation
28. ✅ Add CLI integration tests (`pkg/cmd/flags_test.go`)

### Phase 7: Release Storage Enhancement ✅
29. ✅ Add `SequencingMetadata` field to `Release` struct in `pkg/release/v1/release.go`
30. ✅ Implement manifest-based DAG reconstruction in `pkg/sequencing/metadata.go`
31. ✅ Add robust subchart identification from Source comments with nested support
32. ✅ Handle post-renderer edge cases with fallback strategies
33. ✅ Ensure rollback operations can reconstruct DAG from stored manifest
34. ✅ Fix: rollback now preserves `SequencingMetadata` from previous release
35. ✅ Add unit tests for manifest-based DAG reconstruction

### Phase 8: Testing and Documentation ✅
36. ✅ Add comprehensive unit tests for all new functionality (37 sequencing tests)
37. ✅ Add integration tests for end-to-end subchart sequencing (install, upgrade, rollback, uninstall)
38. ✅ Add performance benchmarks (DAG ~27µs/100nodes, topo sort ~15µs, partition ~171µs)
39. ✅ Test manifest-based DAG reconstruction with various post-renderer scenarios
40. ✅ Add backward compatibility tests (v2 charts, old releases, --wait without =ordered)
41. Remaining: User documentation with examples
42. Remaining: Troubleshooting guide for common sequencing issues

### Phase 9: Validation and Polish ✅
43. ✅ Validate backward compatibility with existing charts
44. ✅ Performance benchmarks confirm <5% overhead
45. ✅ Fix lint failures (self-assignment bug, gofmt, unused params)
46. Remaining: Debug visualization command (`helm dependency graph`)
47. Remaining: Release notes and migration guide

### Phase 10: Remaining HIP-0025 milestones from project plan gist
48. Remaining: Resource-group annotation parser (`helm.sh/resource-group`)
49. Remaining: Resource-group dependency annotation parser (`helm.sh/depends-on/resource-groups`)
50. Remaining: Resource-group DAG construction + validation tests
51. Remaining: Readiness framework gaps (kstatus integration, custom readiness annotations, JSONPath evaluator)
52. Remaining: Add `--readiness-timeout` CLI/SDK surface and compatibility tests
53. Remaining: Integrate resource-level sequencing with subchart sequencing
54. Remaining: Error/warning scenarios for missing dependency groups and isolated groups
55. Remaining: Full-suite validation milestones (stress/UAT + example sequencing charts)
56. Remaining: Security review, final integration review, release candidate prep
57. Remaining: Beta/GA rollout milestones (feedback loop, critical fixes, release notes/announcements)

### Supplemental milestones from gist (tracking)
- Remaining: Infrastructure readiness (CI/CD enhancements, test clusters, performance test infra)
- Remaining: External dependency decisions (kstatus, JSONPath library selection)
- Remaining: Communication cadence (maintainer updates, community beta coordination)

## Success Criteria

- ✅ 90%+ test coverage for all new functionality
- ✅ Performance overhead < 5% compared to current implementation
- ✅ Zero breaking changes to existing chart functionality
- ✅ Support for both annotation-based and field-based dependency declaration
- ✅ Comprehensive error handling with clear user messages
- ✅ Full backward compatibility with Helm v3 charts
- ✅ Robust DAG reconstruction from manifests for rollback scenarios
- ✅ Post-renderer compatibility with fallback strategies
- ✅ Correct handling of nested subchart dependencies

## Risk Mitigation

- **Circular Dependencies**: Implement robust detection with clear error messages
- **Performance Impact**: Use caching and optimize DAG construction
- **Backward Compatibility**: Extensive testing with existing charts
- **Complex Error Scenarios**: Comprehensive error handling and rollback logic
- **Memory Usage**: Monitor and optimize memory consumption during processing
- **Post-Renderer Compatibility**: Multiple fallback strategies for subchart identification
- **Nested Subchart Complexity**: Proper path parsing to handle arbitrary nesting levels
- **Manifest Corruption**: Robust error handling when Source comments are missing

## Review

This implementation plan provides a comprehensive roadmap for adding subchart sequencing support to Helm v4. The plan is structured in phases to enable incremental development and testing, with clear success criteria and risk mitigation strategies.

### Key Updates Based on Analysis:

1. **Manifest-Based DAG Reconstruction**: The plan now includes robust DAG reconstruction from stored manifests, enabling rollback scenarios to maintain subchart sequencing even after post-renderer modifications.

2. **Post-Renderer Compatibility**: Added comprehensive strategies for handling post-renderer scenarios, including fallback mechanisms when Source comments are modified or removed.

3. **Nested Subchart Support**: Enhanced subchart path parsing to correctly identify nested subcharts (e.g., "sentinel" within "redis") rather than just the top-level parent.

4. **Enhanced Release Storage**: Modified the release storage approach to include minimal sequencing metadata while leveraging manifest analysis for DAG reconstruction.

5. **Robust Error Handling**: Added comprehensive error handling for edge cases including missing Source comments, disabled subcharts, and post-renderer modifications.

The implementation maintains backward compatibility while providing robust subchart sequencing capabilities that work reliably across various deployment scenarios.
