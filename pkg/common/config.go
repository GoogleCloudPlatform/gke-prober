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

package common

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultMode           = "cluster"
	DefaultReportInterval = time.Duration(1 * time.Minute)
	ModeNode              = "node"
	ModeCluster           = "cluster"

	UserAgent = "google-pso-tool/gke-prober/v0.0.2"

	WarmingPeriod = 5 * time.Minute

	AddonNameAnnotation    = "components.gke.io/component-name"
	AddonVersionAnnotation = "components.gke.io/component-version"
)

var MetricPrefix string

type Config struct {
	ProjectID      string
	Location       string
	Cluster        string
	Mode           string
	NodeName       string
	NodeIP         string
	Nodepool       string
	HostNetwork    bool
	ReportInterval time.Duration
	ConnProbes     bool
	ClusterProbes  bool
	NodeProbes     bool
	UserAgent      string
	MetricPrefix   string
}

type Addon struct {
	Name    string
	Kind    string
	Version string
}

func (a Addon) String() string {
	return fmt.Sprintf("[%s: %s (%s)]", a.Kind, a.Name, a.Version)
}

// AddonFromPod returns an Addon from a pod, and whether an Addon was successfully identified.
func AddonFromPod(p *v1.Pod) (Addon, bool) {
	addonName, addonVersion, ok := addonMetaFromObjectMeta(p.ObjectMeta)
	if !ok {
		return Addon{}, false
	}

	// TODO: recursively get owner references up to root
	for _, ref := range p.OwnerReferences {
		if ref.APIVersion == "apps/v1" && *ref.Controller == true {
			if ref.Kind == "ReplicaSet" {
				return Addon{
					Name:    addonName,
					Kind:    "Deployment",
					Version: addonVersion,
				}, true
			} else {
				return Addon{
					Name:    addonName,
					Kind:    ref.Kind,
					Version: addonVersion,
				}, true
			}
		}
	}
	return Addon{}, false
}

func AddonFromDaemonSet(d *appsv1.DaemonSet) (Addon, bool) {
	addonName, addonVersion, ok := addonMetaFromObjectMeta(d.Spec.Template.ObjectMeta)
	if !ok {
		return Addon{}, false
	}

	return Addon{
		Name:    addonName,
		Kind:    "DaemonSet",
		Version: addonVersion,
	}, true
}

func AddonFromDeployment(d *appsv1.Deployment) (Addon, bool) {
	addonName, addonVersion, ok := addonMetaFromObjectMeta(d.Spec.Template.ObjectMeta)
	if !ok {
		return Addon{}, false
	}

	return Addon{
		Name:    addonName,
		Kind:    "Deployment",
		Version: addonVersion,
	}, true
}

// AddonMetaFromObjectMeta returns the addon Name, Version, and success.
func addonMetaFromObjectMeta(m metav1.ObjectMeta) (string, string, bool) {
	if !metav1.HasAnnotation(m, AddonNameAnnotation) || !metav1.HasAnnotation(m, AddonVersionAnnotation) {
		return "", "", false
	}
	return m.Annotations[AddonNameAnnotation], m.Annotations[AddonVersionAnnotation], true
}

func GetConfig() Config {
	mode := os.Getenv("PROBER_MODE")
	if mode == "" {
		mode = DefaultMode
	}
	nodeIP := os.Getenv("NODE_IP")
	if mode == ModeNode && nodeIP == "" {
		panic(errors.New("can't determine node IP for node mode"))
	}
	hostNetwork := os.Getenv("POD_IP") == nodeIP
	reportInterval, _ := time.ParseDuration(os.Getenv("REPORT_INTERVAL"))
	if reportInterval == 0 {
		reportInterval = DefaultReportInterval
	}

	// Enable Cluster Prober to probe the cluster-wide addon services like metrics-server, kube-dns and so on
	clusterProbes := os.Getenv("ENABLE_CLUSTER_PROBES") == "true"
	nodeProbes := os.Getenv("ENABLE_NODE_PROBES") == "true"
	connProbes := os.Getenv("ENABLE_CONNECTIVITY_PROBES") == "true"
	projectID, location, clusterName, nodeName := getMetadata(mode)

	pool := strings.TrimPrefix(nodeName, fmt.Sprintf("gke-%s-", clusterName))
	// Strip off ending node identifiers ("-13a25f43-chwu")
	pool = pool[:len(pool)-14]

	return Config{
		ProjectID:      projectID,
		Location:       location,
		Cluster:        clusterName,
		Mode:           mode,
		NodeName:       nodeName,
		NodeIP:         nodeIP,
		Nodepool:       pool,
		HostNetwork:    hostNetwork,
		ReportInterval: reportInterval,
		ConnProbes:     connProbes,
		ClusterProbes:  clusterProbes,
		NodeProbes:     nodeProbes,
		UserAgent:      UserAgent,
		MetricPrefix:   MetricPrefix,
	}
}

// Returns best effort for: project, location, cluster name
func getMetadata(mode string) (project, location, cluster, nodename string) {
	project, _ = metadata.ProjectID()
	location = "local"
	switch mode {
	case ModeCluster:
		// For regional clusters, this will be a region rather than zone
		location, _ = metadata.InstanceAttributeValue("cluster-location")
	case ModeNode:
		location, _ = metadata.Zone()
	}
	cluster, _ = metadata.InstanceAttributeValue("cluster-name")
	// We would normally use metadata.InstanceName() but alas,
	// that is not available via the GKE metadata server
	var err error
	if nodename, err = metadata.Hostname(); err != nil {
		panic(fmt.Errorf("can't determine hostname from metadata server: %v\n", err))
	}
	// Remove domain portions
	nodename = strings.Split(nodename, ".")[0]
	return
}

func init() {
	if prefix, ok := os.LookupEnv("METRIC_PREFIX"); ok {
		MetricPrefix = "workload.googleapis.com/" + prefix
	} else {
		MetricPrefix = "workload.googleapis.com/gke-prober"
	}
}
