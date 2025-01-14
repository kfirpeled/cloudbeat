// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package factory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/elastic/cloudbeat/dataprovider/providers/cloud"
	"github.com/elastic/cloudbeat/resources/fetching"
	"github.com/elastic/cloudbeat/resources/utils/testhelper"
)

func TestNewCisAwsOrganizationFactory_Leak(t *testing.T) {
	t.Run("drain", func(t *testing.T) {
		subtest(t, true)
	})
	t.Run("no drain", func(t *testing.T) {
		subtest(t, false)
	})
}

func subtest(t *testing.T, drain bool) {
	const (
		nAccounts           = 5
		nFetchers           = 33
		resourcesPerAccount = 111
	)

	var accounts []AwsAccount
	for i := 0; i < nAccounts; i++ {
		accounts = append(accounts, AwsAccount{
			Identity: cloud.Identity{
				Account:      fmt.Sprintf("account-%d", i),
				AccountAlias: fmt.Sprintf("alias-%d", i),
			},
		})
	}

	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	ctx, cancel := context.WithCancel(context.Background())

	factory := mockFactory(nAccounts,
		func(_ *logp.Logger, _ aws.Config, ch chan fetching.ResourceInfo, _ *cloud.Identity) FetchersMap {
			if drain {
				// create some resources if we are testing for that
				go func() {
					for i := 0; i < resourcesPerAccount; i++ {
						ch <- fetching.ResourceInfo{
							Resource:      mockResource(),
							CycleMetadata: fetching.CycleMetadata{Sequence: int64(i)},
						}
					}
				}()
			}

			fm := FetchersMap{}
			for i := 0; i < nFetchers; i++ {
				fm[fmt.Sprintf("fetcher-%d", i)] = RegisteredFetcher{}
			}
			return fm
		},
	)

	rootCh := make(chan fetching.ResourceInfo)
	fetcherMap := newCisAwsOrganizationFactory(ctx, testhelper.NewLogger(t), rootCh, accounts, factory)
	assert.Lenf(t, fetcherMap, nFetchers*nAccounts, "Correct amount of fetchers")

	if drain {
		expectedResources := nAccounts * resourcesPerAccount
		resources := testhelper.CollectResourcesWithTimeout(rootCh, expectedResources, 1*time.Second)
		assert.Lenf(
			t,
			resources,
			expectedResources,
			"Correct amount of resources fetched",
		)
		defer func() {
			assert.Emptyf(
				t,
				testhelper.CollectResourcesWithTimeout(rootCh, 1, 100*time.Millisecond),
				"Channel not drained",
			)
		}()

		nameCounts := make(map[string]int)
		for _, resource := range resources {
			assert.NotNil(t, resource.GetData())
			assert.NotNil(t, resource.GetElasticCommonData())
			mdata, err := resource.GetMetadata()
			require.NotNil(t, mdata)
			require.NoError(t, err)
			assert.Equal(t, "some-region", mdata.Region)
			assert.NotEqual(t, "some-id", mdata.AwsAccountId)
			assert.NotEqual(t, "some-alias", mdata.AwsAccountAlias)
			nameCounts[mdata.AwsAccountId]++
			nameCounts[mdata.AwsAccountAlias]++
		}
		assert.Len(t, nameCounts, 2*nAccounts)
		for _, v := range nameCounts {
			assert.Equal(t, resourcesPerAccount, v)
		}
	}

	cancel()
}

func TestNewCisAwsOrganizationFactory_LeakContextDone(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx, cancel := context.WithCancel(context.Background())

	newCisAwsOrganizationFactory(
		ctx,
		testhelper.NewLogger(t),
		make(chan fetching.ResourceInfo),
		[]AwsAccount{{
			Identity: cloud.Identity{
				Account:      "1",
				AccountAlias: "account",
			},
		}},
		mockFactory(1,
			func(_ *logp.Logger, _ aws.Config, ch chan fetching.ResourceInfo, _ *cloud.Identity) FetchersMap {
				ch <- fetching.ResourceInfo{
					Resource:      mockResource(),
					CycleMetadata: fetching.CycleMetadata{Sequence: 1},
				}

				return FetchersMap{"fetcher": RegisteredFetcher{}}
			},
		),
	)

	cancel()
}

func TestNewCisAwsOrganizationFactory_CloseChannel(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	newCisAwsOrganizationFactory(
		context.Background(),
		testhelper.NewLogger(t),
		make(chan fetching.ResourceInfo),
		[]AwsAccount{{
			Identity: cloud.Identity{
				Account:      "1",
				AccountAlias: "account",
			},
		}},
		mockFactory(1,
			func(_ *logp.Logger, _ aws.Config, ch chan fetching.ResourceInfo, _ *cloud.Identity) FetchersMap {
				defer close(ch)
				return FetchersMap{"fetcher": RegisteredFetcher{}}
			},
		),
	)
}

func mockResource() *fetching.MockResource {
	m := fetching.MockResource{}
	m.EXPECT().GetData().Return(struct{}{}).Once()
	m.EXPECT().GetMetadata().Return(fetching.ResourceMetadata{
		Region:          "some-region",
		AwsAccountId:    "some-id",
		AwsAccountAlias: "some-alias",
	}, nil).Once()
	m.EXPECT().GetElasticCommonData().Return(struct{}{}).Once()
	return &m
}

func mockFactory(times int, f awsFactory) awsFactory {
	factory := mockAwsFactory{}
	factory.EXPECT().Execute(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(f).Times(times)
	return factory.Execute
}
