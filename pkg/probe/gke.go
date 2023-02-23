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

package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	clusterMetricsApi = "apis/metrics.k8s.io/v1beta1/namespaces/gke-prober-system/pods/"
	dnsLookupHost     = "kubernetes.default.svc.cluster.local"
)

type PodMetricsList struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Timestamp  time.Time `json:"timestamp"`
		Window     string    `json:"window"`
		Containers []struct {
			Name  string `json:"name"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

func ClusterProbes() ClusterProbeMap {
	return ClusterProbeMap{
		"metrics_server": &metricsServerProbe{},
		"kube_dns":       &kubeDnsProbe{host: dnsLookupHost},
	}
}

type metricsServerProbe struct {
}

type kubeDnsProbe struct {
	host string
}

func (p *metricsServerProbe) Run(ctx context.Context, clientset *kubernetes.Clientset) Result {

	var podmetrics PodMetricsList
	var body []byte
	var err error
	body, err = clientset.RESTClient().Get().AbsPath(clusterMetricsApi).DoRaw(ctx)
	if err != nil {
		klog.Warningf("Get metrics from metrics-server returned error %s\n", err.Error())
		return Result{
			Available: "Unhealthy",
			Err:       err,
		}
	}
	if err = json.Unmarshal(body, &podmetrics); err != nil {
		klog.Warningf("Json parser metrics returned error %s\n", err.Error())
		return Result{
			Available: "Unhealthy",
			Err:       err,
		}
	}

	if len(podmetrics.Items) == 0 {
		err = fmt.Errorf("zero metrics returned\n")
		klog.Warningf("Get metrics from metrics-server returned error %s\n", err.Error())
		return Result{
			Available: "Unhealthy",
			Err:       err,
		}
	}

	str, _ := json.MarshalIndent(podmetrics, "", "\t")
	klog.V(2).Infof("Pod metrics List is %s\n", string(str))
	err = fmt.Errorf("Metrics-server is operational health")
	return Result{
		Available: "Healthy",
		Err:       err,
	}
}

func (p *kubeDnsProbe) Run(context.Context, *kubernetes.Clientset) Result {
	ips, err := net.LookupIP(p.host)
	if err != nil {
		klog.Warningf("Dns Lookup response failed from KubeDNS: %s\n", err.Error())
		return Result{
			Available: "Unhealthy",
			Err:       err,
		}
	}
	if len(ips) == 0 {
		err = fmt.Errorf("no IPs returned in dns lookup response from KubeDNS")
		return Result{
			Available: "Unhealthy",
			Err:       err,
		}
	}

	klog.V(1).Infof("Dns lookup responses from KubeDNS returns  %s\n", ips[0])
	err = fmt.Errorf("KubeDNS is operational health")
	return Result{
		Available: "Healthy",
		Err:       err,
	}
}

func NodeProbes() ProbeMap {
	return ProbeMap{
		"fluentbit":           newFluntdProbe(),
		"gke-metadata-server": newGkeMetadataServerProbe(),
	}
}

func resultFromError(err error) Result {
	return Result{
		Available: AvailableError,
		Err:       err,
	}
}

type fluentdProbe struct {
}

func newFluntdProbe() *fluentdProbe {
	return &fluentdProbe{}
}

func (p *fluentdProbe) Run(ctx context.Context, pod *v1.Pod, addon common.Addon) Result {
	ip := pod.Status.HostIP
	_, err := fetchFluentbitUptime(ip)
	if err != nil {
		err = fmt.Errorf("fetchFluentbitUptime returned unexpected error: %v", err)
		return resultFromError(err)
	}
	return defaultProbeResult
}

func fetchFluentbitUptime(ip string) (string, error) {
	resp, err := http.Get("http://" + ip + ":2020/api/v1/uptime")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

type gkeMetadataServerProbe struct {
}

func newGkeMetadataServerProbe() *gkeMetadataServerProbe {
	return &gkeMetadataServerProbe{}
}

func (p *gkeMetadataServerProbe) Run(ctx context.Context, pod *v1.Pod, addon common.Addon) Result {
	_, err := metadata.Email("default")
	if err != nil {
		err = fmt.Errorf("metadata.Email(default) returned %v", err)
		return resultFromError(err)
	}
	return resultSuccessful
}
