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
	"context"
	"fmt"
	"sync"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/metrics"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/probe"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func RecordClusterMetrics(ctx context.Context, m metrics.ClusterRecorder, nodes []*v1.Node, daemonsets []*appsv1.DaemonSet, deployments []*appsv1.Deployment) {
	// Report on node conditions
	if nodes == nil {
		klog.V(1).Infoln("Informer caches syncing. Skip next scraping cycle and wait until the caches are fully synced")
		return
	}
	conditions := nodeConditions(nodes)
	conditionCounts := []metrics.LabelCount{}
	for _, v := range conditions.Dump() {
		conditionCounts = append(conditionCounts, metrics.LabelCount{
			Labels: v.Key,
			Count:  v.Value,
		})
	}
	m.RecordNodeConditions(ctx, conditionCounts)

	// Report on node availability (SLI)
	availabilities, availableNodes := nodeAvailabilities(nodes)
	availabilityCounts := []metrics.LabelCount{}
	for _, v := range availabilities.Dump() {
		availabilityCounts = append(availabilityCounts, metrics.LabelCount{
			Labels: v.Key,
			Count:  v.Value,
		})
	}
	m.RecordNodeAvailabilities(ctx, availabilityCounts)

	// Report on expected addons
	addonCounts := []metrics.LabelCount{}
	for addon, count := range daemonSetPodCountByAddon(daemonsets) {
		labels := addonLabels(addon)
		// Use _available_ nodes as expected number of nodes used by daemonsets
		addonCounts = append(addonCounts, metrics.LabelCount{
			Labels: labels,
			Count:  availableNodes * count,
		})
	}
	for addon, count := range deploymentPodCountByAddon(deployments) {
		labels := addonLabels(addon)
		addonCounts = append(addonCounts, metrics.LabelCount{
			Labels: labels,
			Count:  count,
		})
	}
	m.RecordAddonCounts(ctx, addonCounts)
}

type NodeScheduler struct {
	cfg common.Config
	mr  metrics.NodeRecorder
	*addonRestarts
}

type addonRestarts struct {
	restarts map[common.Addon]int
	m        sync.Mutex
}

func NewNodeScheduler(mr metrics.NodeRecorder, cfg common.Config) *NodeScheduler {
	return &NodeScheduler{
		cfg:           cfg,
		mr:            mr,
		addonRestarts: &addonRestarts{restarts: make(map[common.Addon]int)},
	}
}

func (ar *addonRestarts) RegisterAddonRestart(a common.Addon) {
	ar.m.Lock()
	defer ar.m.Unlock()

	_, ok := ar.restarts[a]
	if !ok {
		ar.restarts[a] = 1
		return
	}
	ar.restarts[a]++
}

func (ar *addonRestarts) PopAddonRestarts() map[common.Addon]int {
	ar.m.Lock()
	defer ar.m.Unlock()

	results := make(map[common.Addon]int)
	for a, n := range ar.restarts {
		results[a] = n
		delete(ar.restarts, a)
	}

	return results
}

func RecordNodeMetrics(ctx context.Context, mr metrics.NodeRecorder, cfg common.Config, nodes []*v1.Node, pods []*v1.Pod, probes probe.ProbeMap) {
	if nodes == nil {
		klog.V(1).Infoln("nodes is 0, wait until the next scraping cycle")
		return
	}
	node := nodes[0] // node watcher will only watch for a single node

	// Record node conditions
	conditions := conditionStatuses(node.Status.Conditions)
	clabels := []map[string]string{}
	for t, st := range conditions {
		labels := map[string]string{
			"nodepool": cfg.Nodepool,
			"zone":     cfg.Location,
			"type":     t,
			"status":   st,
		}
		clabels = append(clabels, labels)
	}
	mr.RecordNodeConditions(ctx, clabels)

	// Record node availability
	ready, scheduleable, doneWarming := nodeAvailability(node)
	nodeAvailable := ready && scheduleable && doneWarming
	labels := map[string]string{
		"nodepool":     cfg.Nodepool,
		"zone":         cfg.Location,
		"available":    boolToStr(nodeAvailable),
		"ready":        boolToStr(ready),
		"scheduleable": boolToStr(scheduleable),
		"done_warming": boolToStr(doneWarming),
	}
	mr.RecordNodeAvailability(ctx, labels)

	// Record addon availability
	// Addon control plane depends on node availability
	cpAvailable := nodeAvailable
	// Use counts: deployments may sometimes have multiple pods on a single node
	// TODO: probing to handle case of multiple pods on node
	counts := podsByAddon(pods)
	labelCounts := []metrics.LabelCount{}
	for addon, pods := range counts {
		// For now, just probe the first pod
		pod := pods[0]
		running := podIsRunning(pod)

		probeResult := probe.AvailableUnknown
		result := probe.Run(ctx, probes, pod, addon)
		if result.Err != nil {
			klog.Warning(result.Err)
		}
		probeResult = result.Available

		available := running && (probeResult == "True" || probeResult == "Unknown")
		if !available {
			cpAvailable = false
		}
		labels := map[string]string{
			"nodepool":       cfg.Nodepool,
			"zone":           cfg.Location,
			"available":      boolToStr(available),
			"node_available": boolToStr(nodeAvailable),
			"running":        boolToStr(running),
			"healthy":        probeResult,
		}
		addAddonLabels(addon, labels)
		labelCounts = append(labelCounts, metrics.LabelCount{
			Labels: labels,
			Count:  len(pods),
		})
	}
	mr.RecordAddonAvailabilies(ctx, labelCounts)

	// Record addon control plane availability
	labels = map[string]string{
		"nodepool":  cfg.Nodepool,
		"zone":      cfg.Location,
		"available": boolToStr(cpAvailable),
	}
	mr.RecordAddonControlPlaneAvailability(ctx, labels)
}

func (s *NodeScheduler) ContainerRestartHandler(ctx context.Context) (handler func(pod *v1.Pod, status v1.ContainerStatus)) {
	handler = func(pod *v1.Pod, status v1.ContainerStatus) {
		addon, ok := common.AddonFromPod(pod)
		if !ok {
			return
		}
		s.RegisterAddonRestart(addon)
		state := status.LastTerminationState.Terminated
		var reason, exitCode string
		if state != nil {
			klog.V(1).Infof("container %q restarted because of %s (%d)\n", status.Name, state.Reason, state.ExitCode)
			reason = state.Reason
			exitCode = fmt.Sprint(state.ExitCode)
		} else {
			klog.V(1).Infof("container %q restarted but termination reason unknown\n", status.Name)
			reason = "unknown"
			exitCode = "unknown"
		}

		labels := map[string]string{
			"nodepool":       s.cfg.Nodepool,
			"zone":           s.cfg.Location,
			"container_name": status.Name,
			"reason":         reason,
			"exit_code":      exitCode,
		}
		addAddonLabels(addon, labels)
		s.mr.RecordContainerRestart(ctx, labels)
	}
	return
}

func RecordClusterProbeMetrics(ctx context.Context, clientset *kubernetes.Clientset,
	recorder metrics.ProbeRecorder, probes probe.ClusterProbeMap) {

	var res probe.Result
	clabels := []map[string]string{}
	for addon, probe := range probes {
		res = probe.Run(ctx, clientset)
		labels := map[string]string{
			"name":      addon,
			"condition": res.Available,
			"reason":    res.Err.Error(),
		}
		clabels = append(clabels, labels)
	}
	recorder.RecordAddonHealth(ctx, clabels)
}

func runConnectivityProbes(ctx context.Context, recorder metrics.ProbeRecorder, probes probe.ConnectivityProbeMap) {
	for name, pr := range probes {
		err := pr.Run(ctx, recorder)
		if err != nil {
			klog.Warningf("probe %q returned unexpected error %v\n", name, err)
		}
	}
}
