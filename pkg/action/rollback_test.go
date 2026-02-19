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
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/sequencing"
)

func TestNewRollback(t *testing.T) {
	config := actionConfigFixture(t)
	client := NewRollback(config)

	assert.NotNil(t, client)
	assert.Equal(t, config, client.cfg)
}

func TestRollbackRun_UnreachableKubeClient(t *testing.T) {
	config := actionConfigFixture(t)
	failingKubeClient := kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard}, DummyResources: nil}
	failingKubeClient.ConnectionError = errors.New("connection refused")
	config.KubeClient = &failingKubeClient

	client := NewRollback(config)
	assert.Error(t, client.Run(""))
}

func TestRollback_WaitOptionsPassedDownstream(t *testing.T) {
	is := assert.New(t)
	config := actionConfigFixture(t)

	// Create a deployed release and a second version to roll back to
	rel := releaseStub()
	rel.Name = "wait-options-rollback"
	rel.Info.Status = "deployed"
	rel.ApplyMethod = "csa"
	require.NoError(t, config.Releases.Create(rel))

	rel2 := releaseStub()
	rel2.Name = "wait-options-rollback"
	rel2.Version = 2
	rel2.Info.Status = "deployed"
	rel2.ApplyMethod = "csa"
	require.NoError(t, config.Releases.Create(rel2))

	client := NewRollback(config)
	client.Version = 1
	client.WaitStrategy = kube.StatusWatcherStrategy
	client.ServerSideApply = "auto"

	// Use WithWaitContext as a marker WaitOption that we can track
	ctx := context.Background()
	client.WaitOptions = []kube.WaitOption{kube.WithWaitContext(ctx)}

	// Access the underlying FailingKubeClient to check recorded options
	failer := config.KubeClient.(*kubefake.FailingKubeClient)

	err := client.Run(rel.Name)
	is.NoError(err)

	// Verify that WaitOptions were passed to GetWaiter
	is.NotEmpty(failer.RecordedWaitOptions, "WaitOptions should be passed to GetWaiter")
}

func TestRollback_PreservesSequencingMetadata(t *testing.T) {
	is := assert.New(t)
	config := actionConfigFixture(t)

	// Create version 1 with sequencing metadata
	meta := &sequencing.SequencingMetadata{
		SubchartOrder: [][]string{{"db"}, {"cache"}, {"web"}, {"parent"}},
		Dependencies: map[string][]string{
			"cache":  {"db"},
			"web":    {"cache"},
			"parent": {"db", "cache", "web"},
		},
	}
	raw, err := meta.Marshal()
	require.NoError(t, err)

	rel1 := releaseStub()
	rel1.Name = "rollback-sequencing"
	rel1.Info.Status = "deployed"
	rel1.ApplyMethod = "csa"
	rel1.SequencingMetadata = raw
	require.NoError(t, config.Releases.Create(rel1))

	// Create version 2 (current, no sequencing metadata)
	rel2 := releaseStub()
	rel2.Name = "rollback-sequencing"
	rel2.Version = 2
	rel2.Info.Status = "deployed"
	rel2.ApplyMethod = "csa"
	rel2.SequencingMetadata = nil
	require.NoError(t, config.Releases.Create(rel2))

	// Rollback to version 1
	client := NewRollback(config)
	client.Version = 1
	client.WaitStrategy = kube.StatusWatcherStrategy
	client.ServerSideApply = "auto"

	err = client.Run(rel1.Name)
	is.NoError(err)

	// Verify the rolled-back release (version 3) gets created
	rolledBacki, err := config.Releases.Get(rel1.Name, 3)
	is.NoError(err)
	rolledBack, err := releaserToV1Release(rolledBacki)
	is.NoError(err)

	// The rolled-back release should have the sequencing metadata from v1
	is.NotNil(rolledBack.SequencingMetadata, "rolled back release should preserve sequencing metadata")
	restoredMeta, err := sequencing.UnmarshalSequencingMetadata(rolledBack.SequencingMetadata)
	is.NoError(err)
	is.Equal(meta.SubchartOrder, restoredMeta.SubchartOrder)
}
