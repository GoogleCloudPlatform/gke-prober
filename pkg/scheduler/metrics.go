// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scheduler

import (
	"reflect"
	"time"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

type mapValue struct {
	Key   map[string]string
	Value int
}
type MapCounter interface {
	Increment(m map[string]string, c int)
	Dump() []mapValue
}
type mapCounter struct {
	values []mapValue
}

func newMapCounter() *mapCounter {
	return &mapCounter{
		values: []mapValue{},
	}
}

func (mc *mapCounter) Increment(m map[string]string, c int) {
	for i, v := range mc.values {
		if reflect.DeepEqual(v.Key, m) {
			mc.values[i].Value = v.Value + c
			return
		}
	}
	mc.values = append(mc.values, mapValue{Key: m, Value: c})
}

func (mc *mapCounter) Dump() []mapValue {
	return mc.values
}

func readyCondition(conditions []v1.NodeCondition) string {
	for _, c := range conditions {
		if c.Type == v1.NodeReady {
			return string(c.Status)
		}
	}
	return "unknown"
}

func nodeConditions(l []*v1.Node) *mapCounter {
	mc := newMapCounter()
	for _, node := range l {
		nodepool := getLabel(node.Labels, "cloud.google.com/gke-nodepool")
		zone := getLabel(node.Labels, "topology.kubernetes.io/zone")
		for t, s := range conditionStatuses(node.Status.Conditions) {
			labels := map[string]string{
				"nodepool": nodepool,
				"zone":     zone,
				"type":     t,
				"status":   s,
			}
			mc.Increment(labels, 1)
		}
	}
	return mc
}

func conditionStatuses(conditions []v1.NodeCondition) map[string]string {
	m := map[string]string{}
	for _, c := range conditions {
		m[string(c.Type)] = string(c.Status)
	}
	return m
}

func nodeAvailabilities(l []*v1.Node) (*mapCounter, int) {
	availableNodes := 0
	mc := newMapCounter()
	for _, node := range l {
		ready, schedulable, doneWarming := nodeAvailability(node)
		available := ready && schedulable && doneWarming
		labels := map[string]string{
			"nodepool":     getLabel(node.Labels, "cloud.google.com/gke-nodepool"),
			"zone":         getLabel(node.Labels, "topology.kubernetes.io/zone"),
			"ready":        boolToStr(ready),
			"schedulable":  boolToStr(schedulable),
			"done_warming": boolToStr(doneWarming),
			"available":    boolToStr(available),
		}
		if available {
			availableNodes++
		}
		mc.Increment(labels, 1)
	}
	return mc, availableNodes
}

func nodesAvailable(l []*v1.Node) int {
	availableNodes := 0
	for _, node := range l {
		ready := nodeIsReady(node)
		scheduleable := nodeIsScheduleable(node)
		doneWarming := nodeIsDoneWarming(node)

		if ready && scheduleable && doneWarming {
			availableNodes++
		}
	}
	return availableNodes
}

func nodeIsReady(node *v1.Node) bool {
	return "True" == readyCondition(node.Status.Conditions)
}

func nodeIsScheduleable(node *v1.Node) bool {
	for _, t := range node.Spec.Taints {
		if t.Key == v1.TaintNodeUnschedulable {
			return false
		}
	}
	return true
}

func nodeIsDoneWarming(node *v1.Node) bool {
	creationTime := node.ObjectMeta.CreationTimestamp
	now := time.Now()

	return now.Sub(creationTime.Time) >= common.WarmingPeriod
}

func nodeAvailability(node *v1.Node) (ready, scheduleable, doneWarming bool) {
	return nodeIsReady(node), nodeIsScheduleable(node), nodeIsDoneWarming(node)
}

// Return label or "Unknown"
func getLabel(labels map[string]string, key string) string {
	value := "Unknown"
	if _, ok := labels[key]; ok {
		value = labels[key]
	}
	return value
}

func daemonSetPodCountByAddon(l []*appsv1.DaemonSet) map[common.Addon]int {
	podCounts := map[common.Addon]int{}
	for _, d := range l {
		if d.Status.DesiredNumberScheduled != 0 {
			if a, ok := common.AddonFromDaemonSet(d); ok {
				podCounts[a] = podCounts[a] + 1
			}
		}
	}
	return podCounts
}

func deploymentPodCountByAddon(l []*appsv1.Deployment) map[common.Addon]int {
	podCounts := map[common.Addon]int{}
	for _, d := range l {
		if *d.Spec.Replicas > 0 {
			if a, ok := common.AddonFromDeployment(d); ok {
				podCounts[a] = podCounts[a] + int(*d.Spec.Replicas)
			}
		}
	}
	return podCounts
}

func podsByAddon(pods []*v1.Pod) map[common.Addon][]*v1.Pod {
	addonPods := map[common.Addon][]*v1.Pod{}
	for _, pod := range pods {
		addon, ok := common.AddonFromPod(pod)
		if !ok {
			continue
		}
		if _, ok := addonPods[addon]; !ok {
			addonPods[addon] = []*v1.Pod{}
		}
		addonPods[addon] = append(addonPods[addon], pod)
	}
	return addonPods
}

func podIsRunning(pod *v1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == v1.PodReady {
			return c.Status == "True"
		}
	}
	return false
}

// formatter specifying capitalized values, aligning with internal k8s style
func boolToStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func addAddonLabels(addon common.Addon, m map[string]string) {
	m["addon"] = addon.Name
	m["version"] = addon.Version
	m["controller"] = addon.Kind
}

func addonLabels(addon common.Addon) map[string]string {
	m := map[string]string{}
	addAddonLabels(addon, m)
	return m
}
