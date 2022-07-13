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

package metrics

import (
	"time"
)

const (
	// MetricPrefix = "workload.googleapis.com/gke-prober"

	MetricClusterAddonsExpected = "cluster/addons_expected"
	MetricClusterNodeAvailable  = "cluster/node_available"
	MetricClusterNodeCondition  = "cluster/node_condition"
	MetricAddonAvailable        = "addon/available"
	MetricAddonCPAvailable      = "addon/control_plane_available"
	MetricAddonRestart          = "addon/restart"
	MetricNodeAvailable         = "node/available"
	MetricNodeCondition         = "node/condition"
)

type LabelCount struct {
	Labels map[string]string
	Count  int
}
type Provider interface {
	ClusterRecorder() ClusterRecorder
	NodeRecorder() NodeRecorder
	ProbeRecorder() ProbeRecorder
}
type ClusterRecorder interface {
	// cluster/addons_expected
	// Entity: k8s_cluster
	// Label: name / Value: (name of addon)
	// Label: version / Value: (version of addon)
	// Label: controller / Value: (DaemonSet or Deployment)
	// Value: count of addons
	RecordAddonCounts(counts []LabelCount)

	// cluster/node_available (SLI)
	// Entity: k8s_cluster
	// Label: nodepool / Values: (set of nodepools)
	// Label: zone / Values: (set of zones)
	// Label: available / Value (True/False)
	// Label: ready / Value (True/False)
	// Label: schedulable / Value (True/False)
	// Label: done_warming / Value (True/False)
	// Value: count of available nodes
	RecordNodeAvailabilities(counts []LabelCount)

	// cluster/node_condition
	// Entity: k8s_cluster
	// Label: nodepool / Values: (set of nodepools)
	// Label: zone / Values: (set of zones)
	// Label: type / Values: (set of conditions)
	// Label: status / Values: (True/False/Unknown)
	// Value: count of nodes reflecting condition
	RecordNodeConditions(counts []LabelCount)
}

type NodeRecorder interface {
	// addon/available
	// Entity: k8s_node
	// Label: nodepool / Value (name of nodepool)
	// Label: zone / Value (name of zone)
	// Label: name / Value: (name of addon)
	// Label: version / Value: (version of addon)
	// Label: controller / Value: (DaemonSet or Deployment)
	// Label: available / Value (True/False)
	// Label: node_available / Value (True/False)
	// Label: running / Value: (True/False)
	// Label: stable / Value: (True/False)
	// Label: healthy / Value: (True/False/Unknown/Error)
	// Value: 1
	RecordAddonAvailabilies(counts []LabelCount)

	// addon/restart
	// Entity: k8s_node
	// Label: nodepool / Value (name of nodepool)
	// Label: zone / Value (name of zone)
	// Label: name / Value: (name of addon)
	// Label: version / Value: (version of addon)
	// Label: controller / Value: (DaemonSet or Deployment)
	// Label: container_name / Value: (name of container in pod)
	// Label: reason / Value: (reason for restart)
	// Label: exit_code / Value: (exit code)
	// Value: 1
	RecordContainerRestart(labels map[string]string)

	// addon/control_plane_available
	// Entity: k8s_node
	// Label: nodepool / Value (name of nodepool)
	// Label: zone / Value (name of zone)
	// Label: available / Value (True/False)
	// Value: 1
	RecordAddonControlPlaneAvailability(labels map[string]string)

	// node/node_condition
	// Entity: k8s_node
	// Label: nodepool / Value (name of nodepool)
	// Label: zone / Value (name of zone)
	// Label: type / Values: (set of conditions)
	// Label: status / Values: (True/False/Unknown)
	// Value: 1
	RecordNodeConditions(labels []map[string]string)

	// node/available
	// Entity: k8s_node
	// Label: nodepool / Value (name of nodepool)
	// Label: zone / Value (name of zone)
	// Label: available / Value (True/False)
	// Label: ready / Value (True/False)
	// Label: scheduleable / Value (True/False)
	// Label: done_warming / Value (True/False)
	// Value: 1
	RecordNodeAvailability(labels map[string]string)
}

type ProbeRecorder interface {
	RecordDNSLookupLatency(elapsed time.Duration)
	RecordHTTPGetLatency(statusCode int, elapsed time.Duration)
}
