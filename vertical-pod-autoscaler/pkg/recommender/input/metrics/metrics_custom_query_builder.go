/*
Copyright 2023 The Kubernetes Authors.

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

package metrics

import (
	"fmt"
	"strings"

	prommodel "github.com/prometheus/common/model"

	k8sapiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

// batchSize is capped at 500 to avoid DFA state explosion in the M3 queries.
// For a given ns query, caps the number of pods OR'd in the regex podNameLabel,
// e.g. custom_query{namespace='namespace',pod_name=~'pod1|pod2|...|pod500'}.
const batchSize = 500

// podsBatch is a batch of pod names.
type podsBatch []string

// nsQuery wraps a custom resource query with metadata.
type nsQuery struct {
	query              string
	resource           k8sapiv1.ResourceName
	pods               podsBatch
	namespace          string
	containerNameLabel prommodel.LabelName
	podNameLabel       prommodel.LabelName
}

// nsQueryBuilder is an interface for building a custom resource query.
type nsQueryBuilder interface {
	// buildBatch builds a batch of queries for a list of pod names in a namespace.
	buildBatch(podNames []string, namespace string) []nsQuery
	// buildRaw builds a single query for a list of pod names in a namespace.
	buildRaw(podNames []string, namespace string) nsQuery
}

// rssQueryBuilder implements the nsQueryBuilder interface for the RSS metric.
type rssQueryBuilder struct {
	resource           k8sapiv1.ResourceName
	containerNameLabel prommodel.LabelName
	podNameLabel       prommodel.LabelName
}

// jvmHeapCommittedQueryBuilder implements the nsQueryBuilder interface for the JVM Heap Committed metric.
type jvmHeapCommittedQueryBuilder struct {
	resource           k8sapiv1.ResourceName
	containerNameLabel prommodel.LabelName
	podNameLabel       prommodel.LabelName
}

func regexOr(values []string) string {
	return strings.Join(values, "|")
}

func getRSSQuery(containerNameLabel prommodel.LabelName, podNameLabel prommodel.LabelName) nsQueryBuilder {
	return &rssQueryBuilder{
		resource:           k8sapiv1.ResourceName(model.ResourceRSS),
		containerNameLabel: containerNameLabel,
		podNameLabel:       podNameLabel,
	}
}

func getJVMHeapCommittedQuery(containerNameLabel prommodel.LabelName, podNameLabel prommodel.LabelName) nsQueryBuilder {
	return &jvmHeapCommittedQueryBuilder{
		resource:           k8sapiv1.ResourceName(model.ResourceJVMHeapCommitted),
		containerNameLabel: containerNameLabel,
		podNameLabel:       podNameLabel,
	}
}

// batchPodNames splits the list of pod names into batches of batchSize.
func batchPodNames(podNames []string) []podsBatch {
	batches := []podsBatch{}
	for start := 0; start < len(podNames); start += batchSize {
		end := start + batchSize
		if end > len(podNames) {
			end = len(podNames)
		}

		batches = append(batches, podNames[start:end])
	}
	return batches
}

func (r *rssQueryBuilder) buildBatch(podNames []string, namespace string) []nsQuery {
	batches := batchPodNames(podNames)
	queries := []nsQuery{}
	for _, batch := range batches {
		queries = append(queries, r.buildRaw(batch, namespace))
	}
	return queries
}

func (r *rssQueryBuilder) buildRaw(podNames []string, namespace string) nsQuery {
	return nsQuery{
		query:              fmt.Sprintf("max_over_time(container_memory_rss{%s!='', %s=~'%s', namespace='%s'}[5m])", r.containerNameLabel, r.podNameLabel, regexOr(podNames), namespace),
		resource:           r.resource,
		pods:               podNames,
		namespace:          namespace,
		containerNameLabel: r.containerNameLabel,
		podNameLabel:       r.podNameLabel,
	}
}

func (j *jvmHeapCommittedQueryBuilder) buildBatch(podNames []string, namespace string) []nsQuery {
	batches := batchPodNames(podNames)
	queries := []nsQuery{}
	for _, batch := range batches {
		queries = append(queries, j.buildRaw(batch, namespace))
	}
	return queries
}

func (j *jvmHeapCommittedQueryBuilder) buildRaw(podNames []string, namespace string) nsQuery {
	return nsQuery{
		query:              fmt.Sprintf("max_over_time(jmx_Memory_HeapMemoryUsage_committed{%s!='', %s=~'%s', kubernetes_namespace='%s'}[5m])", j.containerNameLabel, j.podNameLabel, regexOr(podNames), namespace),
		resource:           j.resource,
		pods:               podNames,
		namespace:          namespace,
		containerNameLabel: j.containerNameLabel,
		podNameLabel:       j.podNameLabel,
	}
}
