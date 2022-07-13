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
	"fmt"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ModeNode    = "node"
	ModeCluster = "cluster"

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
	Kubeconfig     string
	Mode           string
	NodeName       string
	NodeIP         string
	Nodepool       string
	HostNetwork    bool
	ReportInterval time.Duration
	ConnProbes     bool
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

func init() {
	if prefix, ok := os.LookupEnv("METRIC_PREFIX"); ok {
		MetricPrefix = "workload.googleapis.com/" + prefix
	} else {
		MetricPrefix = "workload.googleapis.com/gke-prober"
	}
}
