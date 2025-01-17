/*
Copyright 2018 The Kubernetes Authors.

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

package oom

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

func init() {
	utilruntime.Must(v1.AddToScheme(scheme))
}

const pod1Yaml = `
apiVersion: v1
kind: Pod
metadata:
  name: Pod1
  namespace: mockNamespace
spec:
  containers:
  - name: Name11
    env:
    - name: OVERRIDE_JVM_HEAP_SIZE
      value: "500M"
    resources:
      requests:
        memory: "1024"
      limits:
        memory: "2048"
status:
  containerStatuses:
  - name: Name11
    restartCount: 0
`

const pod2Yaml = `
apiVersion: v1
kind: Pod
metadata:
  name: Pod1
  namespace: mockNamespace
spec:
  containers:
  - name: Name11
    resources:
      requests:
        memory: "1024"
      limits:
        memory: "2048"
status:
  containerStatuses:
  - name: Name11
    restartCount: 1
    lastState:
      terminated:
        finishedAt: 2018-02-23T13:38:48Z
        reason: OOMKilled
`

const pod3Yaml = `
apiVersion: v1
kind: Pod
metadata:
  name: Pod1
  namespace: mockNamespace
spec:
  containers:
  - name: Name11
    env:
    - name: OVERRIDE_JVM_HEAP_SIZE
      value: "500M"
    resources:
      requests:
        memory: "1024"
      limits:
        memory: "2048"
status:
  containerStatuses:
  - name: Name11
    restartCount: 1
    lastState:
      terminated:
        finishedAt: 2018-02-23T13:38:48Z
        reason: Error
        message: |
          JVM Heap OOM
`

func newPod(yaml string) (*v1.Pod, error) {
	decode := codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(yaml), nil, nil)
	if err != nil {
		return nil, err
	}
	return obj.(*v1.Pod), nil
}

func newEvent(yaml string) (*v1.Event, error) {
	decode := codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(yaml), nil, nil)
	if err != nil {
		return nil, err
	}
	return obj.(*v1.Event), nil
}

func TestOOMReceived(t *testing.T) {
	p1, err := newPod(pod1Yaml)
	assert.NoError(t, err)
	p2, err := newPod(pod2Yaml)
	assert.NoError(t, err)
	observer := NewObserver()
	go observer.OnUpdate(p1, p2)

	infos := <-observer.observedOomsChannel
	info := infos[0]
	container := info.ContainerID
	assert.Equal(t, "mockNamespace", container.PodID.Namespace)
	assert.Equal(t, "Pod1", container.PodID.PodName)
	assert.Equal(t, "Name11", container.ContainerName)
	assert.Equal(t, model.ResourceAmount(int64(1024)), info.Memory)
	assert.Equal(t, model.ResourceMemory, info.Resource)
	timestamp, err := time.Parse(time.RFC3339, "2018-02-23T13:38:48Z")
	assert.NoError(t, err)
	assert.Equal(t, timestamp.Unix(), info.Timestamp.Unix())

	infos = <-observer.observedOomsChannel
	infoRSS := infos[0]
	container = infoRSS.ContainerID
	assert.Equal(t, "mockNamespace", container.PodID.Namespace)
	assert.Equal(t, "Pod1", container.PodID.PodName)
	assert.Equal(t, "Name11", container.ContainerName)
	assert.Equal(t, model.ResourceAmount(int64(2048)), infoRSS.Memory)
	assert.Equal(t, model.ResourceRSS, infoRSS.Resource)
	timestamp, err = time.Parse(time.RFC3339, "2018-02-23T13:38:48Z")
	assert.NoError(t, err)
	assert.Equal(t, timestamp.Unix(), infoRSS.Timestamp.Unix())
}

func TestJVMHeapOOMReceived(t *testing.T) {
	p1, err := newPod(pod1Yaml)
	assert.NoError(t, err)
	p3, err := newPod(pod3Yaml)
	assert.NoError(t, err)
	observer := NewObserver()
	go observer.OnUpdate(p1, p3)

	infos := <-observer.observedOomsChannel
	infoJVMHeap := infos[0]
	container := infoJVMHeap.ContainerID
	assert.Equal(t, "mockNamespace", container.PodID.Namespace)
	assert.Equal(t, "Pod1", container.PodID.PodName)
	assert.Equal(t, "Name11", container.ContainerName)
	val, err := resource.ParseQuantity("500Mi")
	assert.NoError(t, err)
	assert.Equal(t, model.ResourceAmount(val.Value()), infoJVMHeap.Memory)
	assert.Equal(t, model.ResourceJVMHeapCommitted, infoJVMHeap.Resource)
	timestamp, err := time.Parse(time.RFC3339, "2018-02-23T13:38:48Z")
	assert.NoError(t, err)
	assert.Equal(t, timestamp.Unix(), infoJVMHeap.Timestamp.Unix())

	infoRSS := infos[1]
	container = infoRSS.ContainerID
	assert.Equal(t, "mockNamespace", container.PodID.Namespace)
	assert.Equal(t, "Pod1", container.PodID.PodName)
	assert.Equal(t, "Name11", container.ContainerName)
	assert.Equal(t, model.ResourceAmount(int64(2048)), infoRSS.Memory)
	assert.Equal(t, model.ResourceRSS, infoRSS.Resource)
	timestamp, err = time.Parse(time.RFC3339, "2018-02-23T13:38:48Z")
	assert.NoError(t, err)
	assert.Equal(t, timestamp.Unix(), infoRSS.Timestamp.Unix())

}

func TestMalformedPodReceived(t *testing.T) {
	p1, err := newPod(pod1Yaml)
	assert.NoError(t, err)
	p2, err := newPod(pod2Yaml)
	assert.NoError(t, err)

	// Malformed pod: restart count > 0, but last termination status is nil
	p2.Status.ContainerStatuses[0].RestartCount = 1
	p2.Status.ContainerStatuses[0].LastTerminationState.Terminated = nil

	observer := NewObserver()
	observer.OnUpdate(p1, p2)
	assert.Empty(t, observer.observedOomsChannel)
}

func TestParseEvictionEvent(t *testing.T) {
	parseTimestamp := func(str string) time.Time {
		timestamp, err := time.Parse(time.RFC3339, "2018-02-23T13:38:48Z")
		assert.NoError(t, err)
		return timestamp.UTC()
	}
	parseResources := func(str string) model.ResourceAmount {
		memory, err := resource.ParseQuantity(str)
		assert.NoError(t, err)
		return model.ResourceAmount(memory.Value())
	}

	toContainerID := func(namespace, pod, container string) model.ContainerID {
		return model.ContainerID{
			PodID: model.PodID{
				PodName:   pod,
				Namespace: namespace,
			},
			ContainerName: container,
		}
	}

	testCases := []struct {
		event   string
		oomInfo []OomInfo
	}{
		{
			event: `
apiVersion: v1
kind: Event
metadata:
  annotations:
    offending_containers: test-container
    offending_containers_usage: 1024Ki
    starved_resource: memory
  creationTimestamp: 2018-02-23T13:38:48Z
involvedObject:
  apiVersion: v1
  kind: Pod
  name: pod1
  namespace: test-namespace
reason: Evicted
`,
			oomInfo: []OomInfo{
				{
					Timestamp:   parseTimestamp("2018-02-23T13:38:48Z "),
					Memory:      parseResources("1024Ki"),
					Resource:    model.ResourceMemory,
					ContainerID: toContainerID("test-namespace", "pod1", "test-container"),
				},
			},
		},
		{
			event: `
apiVersion: v1
kind: Event
metadata:
  annotations:
    offending_containers: test-container,other-container
    offending_containers_usage: 1024Ki,2048Ki
    starved_resource: memory,memory
  creationTimestamp: 2018-02-23T13:38:48Z
involvedObject:
  apiVersion: v1
  kind: Pod
  name: pod1
  namespace: test-namespace
reason: Evicted
`,
			oomInfo: []OomInfo{
				{
					Timestamp:   parseTimestamp("2018-02-23T13:38:48Z "),
					Memory:      parseResources("1024Ki"),
					Resource:    model.ResourceMemory,
					ContainerID: toContainerID("test-namespace", "pod1", "test-container"),
				},
				{
					Timestamp:   parseTimestamp("2018-02-23T13:38:48Z "),
					Memory:      parseResources("2048Ki"),
					Resource:    model.ResourceMemory,
					ContainerID: toContainerID("test-namespace", "pod1", "other-container"),
				},
			},
		},
		{
			event: `
apiVersion: v1
kind: Event
metadata:
  annotations:
    offending_containers: test-container,other-container
    offending_containers_usage: 1024Ki,2048Ki
    starved_resource: memory,evictable                       # invalid resource skipped
  creationTimestamp: 2018-02-23T13:38:48Z
involvedObject:
  apiVersion: v1
  kind: Pod
  name: pod1
  namespace: test-namespace
reason: Evicted
`,
			oomInfo: []OomInfo{
				{
					Timestamp:   parseTimestamp("2018-02-23T13:38:48Z "),
					Memory:      parseResources("1024Ki"),
					Resource:    model.ResourceMemory,
					ContainerID: toContainerID("test-namespace", "pod1", "test-container"),
				},
			},
		},
		{
			event: `
apiVersion: v1
kind: Event
metadata:
  annotations:
    offending_containers: test-container,other-container
    offending_containers_usage: 1024Ki,2048Ki
    starved_resource: memory                              # missing resource invalids all event
  creationTimestamp: 2018-02-23T13:38:48Z
involvedObject:
  apiVersion: v1
  kind: Pod
  name: pod1
  namespace: test-namespace
reason: Evicted
`,
			oomInfo: []OomInfo{},
		},
	}

	for _, tc := range testCases {
		event, err := newEvent(tc.event)
		assert.NoError(t, err)
		assert.NotNil(t, event)

		oomInfoArray := parseEvictionEvent(event)
		assert.Equal(t, tc.oomInfo, oomInfoArray)
	}
}

func TestFindContainerOverrideJvmHeapSizeEnv(t *testing.T) {
	// Define the test cases in a table
	tests := []struct {
		name     string
		envVars  []v1.EnvVar
		expected *resource.Quantity
	}{
		{
			name: "Valid MiB heap size",
			envVars: []v1.EnvVar{
				{
					Name:  "OVERRIDE_JVM_HEAP_SIZE",
					Value: "512m",
				},
			},
			expected: func() *resource.Quantity {
				q, _ := resource.ParseQuantity("512Mi")
				return &q
			}(),
		},
		{
			name: "Valid GiB heap size",
			envVars: []v1.EnvVar{
				{
					Name:  "OVERRIDE_JVM_HEAP_SIZE",
					Value: "2g",
				},
			},
			expected: func() *resource.Quantity {
				q, _ := resource.ParseQuantity("2Gi")
				return &q
			}(),
		},
		{
			name: "Valid raw byte heap size",
			envVars: []v1.EnvVar{
				{
					Name:  "OVERRIDE_JVM_HEAP_SIZE",
					Value: "1048576",
				},
			},
			expected: func() *resource.Quantity {
				q, _ := resource.ParseQuantity("1048576")
				return &q
			}(),
		},
		{
			name: "Invalid non-numeric heap size",
			envVars: []v1.EnvVar{
				{
					Name:  "OVERRIDE_JVM_HEAP_SIZE",
					Value: "invalid",
				},
			},
			expected: nil,
		},
		{
			name: "Empty heap size value",
			envVars: []v1.EnvVar{
				{
					Name:  "OVERRIDE_JVM_HEAP_SIZE",
					Value: "",
				},
			},
			expected: nil,
		},
		{
			name: "Heap size not set",
			envVars: []v1.EnvVar{
				{
					Name:  "SOME_OTHER_ENV_VAR",
					Value: "value",
				},
			},
			expected: nil,
		},
	}

	// Loop over each test case
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := findContainerOverrideJvmHeapSizeEnv(tc.envVars)

			// Compare results
			if tc.expected == nil && result != nil {
				t.Errorf("Expected nil, got %v", result)
			} else if tc.expected != nil && result == nil {
				t.Errorf("Expected %v, got nil", tc.expected)
			} else if tc.expected != nil && result != nil && tc.expected.String() != result.String() {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}
