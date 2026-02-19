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

package action

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/kube"
	releasei "helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
	releaseutil "helm.sh/helm/v4/pkg/release/v1/util"
	"helm.sh/helm/v4/pkg/sequencing"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// Uninstall is the action for uninstalling releases.
//
// It provides the implementation of 'helm uninstall'.
type Uninstall struct {
	cfg *Configuration

	DisableHooks        bool
	DryRun              bool
	IgnoreNotFound      bool
	KeepHistory         bool
	WaitStrategy        kube.WaitStrategy
	WaitOptions         []kube.WaitOption
	DeletionPropagation string
	Timeout             time.Duration
	Description         string
}

// NewUninstall creates a new Uninstall object with the given configuration.
func NewUninstall(cfg *Configuration) *Uninstall {
	return &Uninstall{
		cfg: cfg,
	}
}

// Run uninstalls the given release.
func (u *Uninstall) Run(name string) (*releasei.UninstallReleaseResponse, error) {
	if err := u.cfg.KubeClient.IsReachable(); err != nil {
		return nil, err
	}

	var waiter kube.Waiter
	var err error
	if c, supportsOptions := u.cfg.KubeClient.(kube.InterfaceWaitOptions); supportsOptions {
		waiter, err = c.GetWaiterWithOptions(u.WaitStrategy, u.WaitOptions...)
	} else {
		waiter, err = u.cfg.KubeClient.GetWaiter(u.WaitStrategy)
	}
	if err != nil {
		return nil, err
	}

	if u.DryRun {
		ri, err := u.cfg.releaseContent(name, 0)

		if err != nil {
			if u.IgnoreNotFound && errors.Is(err, driver.ErrReleaseNotFound) {
				return nil, nil
			}
			return &releasei.UninstallReleaseResponse{}, err
		}
		r, err := releaserToV1Release(ri)
		if err != nil {
			return nil, err
		}
		return &releasei.UninstallReleaseResponse{Release: r}, nil
	}

	if err := chartutil.ValidateReleaseName(name); err != nil {
		return nil, fmt.Errorf("uninstall: Release name is invalid: %s", name)
	}

	relsi, err := u.cfg.Releases.History(name)
	if err != nil {
		if u.IgnoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("uninstall: Release not loaded: %s: %w", name, err)
	}
	if len(relsi) < 1 {
		return nil, errMissingRelease
	}

	rels, err := releaseListToV1List(relsi)
	if err != nil {
		return nil, err
	}

	releaseutil.SortByRevision(rels)
	rel := rels[len(rels)-1]

	// TODO: Are there any cases where we want to force a delete even if it's
	// already marked deleted?
	if rel.Info.Status == common.StatusUninstalled {
		if !u.KeepHistory {
			if err := u.purgeReleases(rels...); err != nil {
				return nil, fmt.Errorf("uninstall: Failed to purge the release: %w", err)
			}
			return &releasei.UninstallReleaseResponse{Release: rel}, nil
		}
		return nil, fmt.Errorf("the release named %q is already deleted", name)
	}

	u.cfg.Logger().Debug("uninstall: deleting release", "name", name)
	rel.Info.Status = common.StatusUninstalling
	rel.Info.Deleted = time.Now()
	rel.Info.Description = "Deletion in progress (or silently failed)"
	res := &releasei.UninstallReleaseResponse{Release: rel}

	if !u.DisableHooks {
		serverSideApply := true
		if err := u.cfg.execHook(rel, release.HookPreDelete, u.WaitStrategy, u.WaitOptions, u.Timeout, serverSideApply); err != nil {
			return res, err
		}
	} else {
		u.cfg.Logger().Debug("delete hooks disabled", "release", name)
	}

	// From here on out, the release is currently considered to be in StatusUninstalling
	// state.
	if err := u.cfg.Releases.Update(rel); err != nil {
		u.cfg.Logger().Debug("uninstall: Failed to store updated release", slog.Any("error", err))
	}

	deletedResources, kept, errs := u.deleteRelease(rel)
	if errs != nil {
		u.cfg.Logger().Debug("uninstall: Failed to delete release", slog.Any("error", errs))
		return nil, fmt.Errorf("failed to delete release: %s", name)
	}

	if kept != "" {
		kept = "These resources were kept due to the resource policy:\n" + kept
	}
	res.Info = kept

	if err := waiter.WaitForDelete(deletedResources, u.Timeout); err != nil {
		errs = append(errs, err)
	}

	if !u.DisableHooks {
		serverSideApply := true
		if err := u.cfg.execHook(rel, release.HookPostDelete, u.WaitStrategy, u.WaitOptions, u.Timeout, serverSideApply); err != nil {
			errs = append(errs, err)
		}
	}

	rel.Info.Status = common.StatusUninstalled
	if len(u.Description) > 0 {
		rel.Info.Description = u.Description
	} else {
		rel.Info.Description = "Uninstallation complete"
	}

	if !u.KeepHistory {
		u.cfg.Logger().Debug("purge requested", "release", name)
		err := u.purgeReleases(rels...)
		if err != nil {
			errs = append(errs, fmt.Errorf("uninstall: Failed to purge the release: %w", err))
		}

		// Return the errors that occurred while deleting the release, if any
		if len(errs) > 0 {
			return res, fmt.Errorf("uninstallation completed with %d error(s): %w", len(errs), joinErrors(errs, "; "))
		}

		return res, nil
	}

	if err := u.cfg.Releases.Update(rel); err != nil {
		u.cfg.Logger().Debug("uninstall: Failed to store updated release", slog.Any("error", err))
	}

	// Supersede all previous deployments, see issue #12556 (which is a
	// variation on #2941).
	deployed, err := u.cfg.Releases.DeployedAll(name)
	if err != nil && !errors.Is(err, driver.ErrNoDeployedReleases) {
		return nil, err
	}
	for _, reli := range deployed {
		rel, err := releaserToV1Release(reli)
		if err != nil {
			return nil, err
		}

		u.cfg.Logger().Debug("superseding previous deployment", "version", rel.Version)
		rel.Info.Status = common.StatusSuperseded
		if err := u.cfg.Releases.Update(rel); err != nil {
			u.cfg.Logger().Debug("uninstall: Failed to store updated release", slog.Any("error", err))
		}
	}

	if len(errs) > 0 {
		return res, fmt.Errorf("uninstallation completed with %d error(s): %w", len(errs), joinErrors(errs, "; "))
	}
	return res, nil
}

func (u *Uninstall) purgeReleases(rels ...*release.Release) error {
	for _, rel := range rels {
		if _, err := u.cfg.Releases.Delete(rel.Name, rel.Version); err != nil {
			return err
		}
	}
	return nil
}

type joinedErrors struct {
	errs []error
	sep  string
}

func joinErrors(errs []error, sep string) error {
	return &joinedErrors{
		errs: errs,
		sep:  sep,
	}
}

func (e *joinedErrors) Error() string {
	errs := make([]string, 0, len(e.errs))
	for _, err := range e.errs {
		errs = append(errs, err.Error())
	}
	return strings.Join(errs, e.sep)
}

func (e *joinedErrors) Unwrap() []error {
	return e.errs
}

// deleteRelease deletes the release and returns list of delete resources and manifests that were kept in the deletion process
func (u *Uninstall) deleteRelease(rel *release.Release) (kube.ResourceList, string, []error) {
	// If the release has sequencing metadata, delete in reverse DAG order
	if len(rel.SequencingMetadata) > 0 {
		return u.deleteReleaseOrdered(rel)
	}

	var errs []error

	manifests := releaseutil.SplitManifests(rel.Manifest)
	_, files, err := releaseutil.SortManifests(manifests, nil, releaseutil.UninstallOrder)
	if err != nil {
		// We could instead just delete everything in no particular order.
		// FIXME: One way to delete at this point would be to try a label-based
		// deletion. The problem with this is that we could get a false positive
		// and delete something that was not legitimately part of this release.
		return nil, rel.Manifest, []error{fmt.Errorf("corrupted release record. You must manually delete the resources: %w", err)}
	}

	filesToKeep, filesToDelete := filterManifestsToKeep(files)
	var kept strings.Builder
	for _, f := range filesToKeep {
		fmt.Fprintf(&kept, "[%s] %s\n", f.Head.Kind, f.Head.Metadata.Name)
	}

	var builder strings.Builder
	for _, file := range filesToDelete {
		builder.WriteString("\n---\n" + file.Content)
	}

	resources, err := u.cfg.KubeClient.Build(strings.NewReader(builder.String()), false)
	if err != nil {
		return nil, "", []error{fmt.Errorf("unable to build kubernetes objects for delete: %w", err)}
	}
	if len(resources) > 0 {
		_, errs = u.cfg.KubeClient.Delete(resources, parseCascadingFlag(u.DeletionPropagation))
	}
	return resources, kept.String(), errs
}

// deleteReleaseOrdered deletes resources in reverse DAG order when sequencing
// metadata is present (HIP-0025). Parent chart resources are deleted first,
// then downstream subcharts in reverse topological order.
func (u *Uninstall) deleteReleaseOrdered(rel *release.Release) (kube.ResourceList, string, []error) {
	meta, err := sequencing.UnmarshalSequencingMetadata(rel.SequencingMetadata)
	if err != nil {
		return nil, "", []error{fmt.Errorf("failed to unmarshal sequencing metadata: %w", err)}
	}

	dag, err := sequencing.ReconstructDAG(meta)
	if err != nil {
		return nil, "", []error{fmt.Errorf("failed to reconstruct DAG from metadata: %w", err)}
	}

	// Reverse the DAG for uninstall order
	revDAG := dag.Reverse()
	batches, err := revDAG.TopologicalSort()
	if err != nil {
		return nil, "", []error{fmt.Errorf("failed to sort reverse DAG: %w", err)}
	}

	// Determine the parent name (last in original order = first in reverse)
	var parentName string
	if len(meta.SubchartOrder) > 0 {
		lastBatch := meta.SubchartOrder[len(meta.SubchartOrder)-1]
		if len(lastBatch) > 0 {
			parentName = lastBatch[0]
		}
	}

	partitions := sequencing.PartitionBySubchart(rel.Manifest, parentName)

	var allResources kube.ResourceList
	var allErrors []error
	var kept strings.Builder

	for _, batch := range batches {
		var batchManifest strings.Builder
		for _, name := range batch {
			if m, ok := partitions[name]; ok {
				if batchManifest.Len() > 0 {
					batchManifest.WriteString("\n---\n")
				}
				batchManifest.WriteString(m)
			}
		}

		if batchManifest.Len() == 0 {
			continue
		}

		resources, err := u.cfg.KubeClient.Build(strings.NewReader(batchManifest.String()), false)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("unable to build resources for batch %v: %w", batch, err))
			continue
		}

		if len(resources) > 0 {
			_, errs := u.cfg.KubeClient.Delete(resources, parseCascadingFlag(u.DeletionPropagation))
			allErrors = append(allErrors, errs...)
			allResources = append(allResources, resources...)
		}

		u.cfg.Logger().Debug("batch deleted", "subcharts", batch)
	}

	return allResources, kept.String(), allErrors
}

func parseCascadingFlag(cascadingFlag string) v1.DeletionPropagation {
	switch cascadingFlag {
	case "orphan":
		return v1.DeletePropagationOrphan
	case "foreground":
		return v1.DeletePropagationForeground
	case "background":
		return v1.DeletePropagationBackground
	default:
		slog.Debug("uninstall: given cascade value, defaulting to delete propagation background", "value", cascadingFlag)
		return v1.DeletePropagationBackground
	}
}
