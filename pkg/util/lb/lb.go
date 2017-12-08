/*
Copyright 2017 Caicloud authors. All rights reserved.

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

package lb

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"time"

	lbapi "github.com/caicloud/clientset/pkg/apis/loadbalance/v1alpha2"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	// NodeUnreachablePodReason is the reason and message set on a pod
	// when its state cannot be confirmed as kubelet is unresponsive
	// on the node it is (was) running.
	// copy from k8s.io/kubernetes/pkg/util/node
	NodeUnreachablePodReason = "NodeLost"
)

// DefaultRetry is the recommended retry for a conflict where multiple clients
// are making changes to the same resource.
var DefaultRetry = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

// SortPodStatusByName ...
type SortPodStatusByName []lbapi.PodStatus

func (s SortPodStatusByName) Len() int {
	return len(s)
}

func (s SortPodStatusByName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

func (s SortPodStatusByName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// SplitNamespaceAndNameByDot returns the namespace and name that
// encoded into the label or value by dot
func SplitNamespaceAndNameByDot(value string) (namespace, name string, err error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected format: %q", value)
	}

	return parts[0], parts[1], nil
}

// ProxyStatusEqual check whether the given two PorxyStatuses are equal
func ProxyStatusEqual(a, b lbapi.ProxyStatus) bool {

	if !PodStatusesEqual(a.PodStatuses, b.PodStatuses) {
		return false
	}
	a.PodStatuses = lbapi.PodStatuses{}
	b.PodStatuses = lbapi.PodStatuses{}
	return reflect.DeepEqual(a, b)
}

// IpvsdrProviderStatusEqual check whether the given two Statuses are equal
func IpvsdrProviderStatusEqual(a, b lbapi.IpvsdrProviderStatus) bool {
	if !PodStatusesEqual(a.PodStatuses, b.PodStatuses) {
		return false
	}
	a.PodStatuses = lbapi.PodStatuses{}
	b.PodStatuses = lbapi.PodStatuses{}
	return reflect.DeepEqual(a, b)
}

// PodStatusesEqual check whether the given two PodStatuses are equal
func PodStatusesEqual(a, b lbapi.PodStatuses) bool {
	aStatus := a.Statuses
	bStatus := b.Statuses

	if len(aStatus) != len(bStatus) {
		return false
	}

	a.Statuses = nil
	b.Statuses = nil

	if !reflect.DeepEqual(a, b) {
		return false
	}

	for _, as := range aStatus {
		equal := false
		for _, bs := range bStatus {
			if as.Name == bs.Name {
				equal = reflect.DeepEqual(as, bs)
				break
			}
		}
		if !equal {
			return false
		}
	}

	return true
}

// CalculateReplicas helps you to calculate replicas of lb
// determines if you need to add node affinity
func CalculateReplicas(lb *lbapi.LoadBalancer) (int32, bool) {
	var replicas int32
	var needNodeAffinity bool

	if lb.Spec.Nodes.Replicas != nil {
		replicas = *lb.Spec.Nodes.Replicas
	}

	if len(lb.Spec.Nodes.Names) != 0 {
		// use nodes length override replicas
		replicas = int32(len(lb.Spec.Nodes.Names))
		needNodeAffinity = true
	}

	return replicas, needNodeAffinity
}

// RandStringBytesRmndr returns a randome string.
func RandStringBytesRmndr(n int) string {
	rand.Seed(int64(time.Now().Nanosecond()))
	var letterBytes = "abcdefghijklmnopqrstuvwxyz1234567890"
	b := make([]byte, n)
	b[0] = letterBytes[rand.Int63()%26]
	for i := 1; i < n; i++ {
		b[i] = letterBytes[rand.Int63()%int64(len(letterBytes))]
	}
	return string(b)
}

// ComputePodStatus computes the pod's current status
func ComputePodStatus(pod *v1.Pod) lbapi.PodStatus {
	restarts := 0
	readyContainers := 0
	totalContainers := len(pod.Spec.Containers)
	reason := string(pod.Status.Phase)
	ready := false
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}

	for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
		container := pod.Status.ContainerStatuses[i]
		restarts += int(container.RestartCount)

		if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
			reason = container.State.Waiting.Reason
		} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
			reason = container.State.Terminated.Reason
		} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
			if container.State.Terminated.Signal != 0 {
				reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
			} else {
				reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
			}
		} else if container.Ready && container.State.Running != nil {
			readyContainers++
		}

	}

	if readyContainers == totalContainers {
		ready = true
	}

	if pod.DeletionTimestamp != nil {
		ready = false
		if pod.Status.Reason == NodeUnreachablePodReason {
			reason = "Unknown"
		} else {
			reason = "Terminating"
		}
	}

	status := lbapi.PodStatus{
		Name:            pod.Name,
		Ready:           ready,
		NodeName:        pod.Spec.NodeName,
		ReadyContainers: int32(readyContainers),
		TotalContainers: int32(totalContainers),
		Reason:          reason,
	}
	return status
}