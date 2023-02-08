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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/healthz"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/k8s"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/metrics"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/probe"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/scheduler"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
)

const (
	DefaultMode = common.ModeCluster

	localPromPort         = ":8080"
	DefaultReportInterval = time.Duration(1 * time.Minute)
)

func main() {
	klog.InitFlags(nil)
	cfg := getConfig()
	klog.Infof("starting with config: %+v\n", cfg)
	// fmt.Printf("starting with config: %+v\n", cfg)

	clientset := k8s.ClientOrDie(cfg.Kubeconfig)

	ctx, cancel := context.WithCancel(context.Background())
	wg := new(sync.WaitGroup)
	wg.Add(1)

	// Initialize metrics pipeline
	var provider metrics.Provider
	var err error
	//provider, err = metrics.StartOTel(ctx, cfg)
	provider, err = metrics.StartGCM(ctx, cfg)
	if err != nil {
		panic(err.Error())
	}

	// initialize watcher, metrics recorder, and prober
	if cfg.Mode == common.ModeCluster {
		cr := provider.ClusterRecorder()
		w := k8s.NewClusterWatcher(clientset)
		w.StartClusterWatches(ctx)
		go scheduler.StartClusterRecorder(ctx, cr, w, cfg.ReportInterval)

		if cfg.ConnProbes {
			pr := provider.ProbeRecorder()
			go scheduler.StartClusterProbes(ctx, clientset, pr, probe.ClusterProbes(), cfg.ReportInterval)
		}
	} else {
		nr := provider.NodeRecorder()
		s := scheduler.NewNodeScheduler(nr, cfg)
		w := k8s.NewNodeWatcher(clientset, cfg.NodeName)
		w.StartNodeWatches(ctx, s.ContainerRestartHandler(ctx))
		go s.StartReporting(ctx, w, probe.NodeProbes(), cfg.ReportInterval)
	}

	//create and launch a server for health-check, including liveness and readiness
	server := &healthz.Server{}
	if err := server.New(nil, nil, ":8081", 60*time.Second); err != nil {
		klog.Warningf("Failed to start the server for health-check, reason: %s", err.Error())
	}
	go server.Start(ctx, wg)

	// Expose prometheus endpoint for local process metrics
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(localPromPort, nil)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	cancel()
	klog.Infoln("shutting down gracefully, waiting internal resources to release and tcp connections to disconnect")
	wg.Wait()
	klog.Infoln("Shutting Down completed, BYE!")
}

func getConfig() common.Config {
	mode, nodeIP, hostNetwork, reportInterval, connProbes := getEnv()
	kubeconfig := getKubeconfig()
	projectID, location, cluster, nodeName := getMetadata(mode)
	nodepool := getNodepool(nodeName, cluster)

	return common.Config{
		ProjectID:      projectID,
		Location:       location,
		Cluster:        cluster,
		Kubeconfig:     kubeconfig,
		Mode:           mode,
		NodeName:       nodeName,
		NodeIP:         nodeIP,
		Nodepool:       nodepool,
		HostNetwork:    hostNetwork,
		ReportInterval: reportInterval,
		ConnProbes:     connProbes,
		UserAgent:      common.UserAgent,
		MetricPrefix:   common.MetricPrefix,
	}
}

// Returns best effort for: project, location, cluster name
func getMetadata(mode string) (project, location, cluster, nodename string) {
	// probe metadata service for project, or fall back to PROJECT_ID in environment
	project = os.Getenv("PROJECT_ID")
	project, _ = metadata.ProjectID()
	location = "local"
	switch mode {
	case common.ModeCluster:
		// For regional clusters, this will be a region rather than zone
		location, _ = metadata.InstanceAttributeValue("cluster-location")
	case common.ModeNode:
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

func getKubeconfig() string {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	return *kubeconfig
}

// TODO: consider better error handling
func getEnv() (mode string, nodeIP string, hostNetwork bool, reportInterval time.Duration, connProbes bool) {
	mode = os.Getenv("PROBER_MODE")
	if mode == "" {
		mode = DefaultMode
	}
	nodeIP = os.Getenv("NODE_IP")
	if mode == common.ModeNode && nodeIP == "" {
		panic(errors.New("can't determine node IP for node mode"))
	}
	hostNetwork = os.Getenv("POD_IP") == nodeIP
	reportInterval, _ = time.ParseDuration(os.Getenv("REPORT_INTERVAL"))
	if reportInterval == 0 {
		reportInterval = DefaultReportInterval
	}

	// Enable Cluster Prober to probe the cluster-wide addon services like metrics-server, kube-dns and so on
	connProbes = os.Getenv("ENABLE_CLUSTER_PROBES") == "true"
	return
}

// extract nodepool from GKE hostname
func getNodepool(nodename, clustername string) string {
	// Remove last two elements
	pool := strings.TrimPrefix(nodename, fmt.Sprintf("gke-%s-", clustername))
	// Strip off ending node identifiers ("-13a25f43-chwu")
	return pool[:len(pool)-14]
}
