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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/k8s"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/localcontroller"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/metrics"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/scheduler"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/util/homedir"
)

var (
	project  string
	location string
	cluster  string
)

func main() {

	flag.Parse()

	if project == "" {
		fmt.Println("Please supply a project id.")
		os.Exit(1)
	}
	if location == "" {
		fmt.Println("Please supply a cluster region/zone.")
		os.Exit(1)
	}
	if cluster == "" {
		fmt.Println("Please supply a cluster name.")
		os.Exit(1)
	}

	cfg := common.Config{
		ProjectID:      project,
		Location:       location,
		Cluster:        cluster,
		Kubeconfig:     getKubeconfig(),
		Mode:           common.ModeCluster,
		NodeName:       "",
		NodeIP:         "",
		Nodepool:       "",
		HostNetwork:    false,
		ReportInterval: time.Duration(1 * time.Minute),
		ConnProbes:     false,
		UserAgent:      common.UserAgent,
		MetricPrefix:   common.MetricPrefix,
	}

	fmt.Printf("starting gke-prober locally with config: %+v\n", cfg)

	clientset := k8s.ClientOrDie(cfg.Kubeconfig)

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()
	defer fmt.Println("exiting")

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
	}

	// Start the local controller to manage node probers
	go localcontroller.StartController(ctx, clientset)

	// Expose prometheus endpoint for local process metrics
	// http.Handle("/metrics", promhttp.Handler())
	// go http.ListenAndServe(localPromPort, nil)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

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

func init() {
	flag.StringVar(&project, "project", "", "your GCP project id")
	flag.StringVar(&location, "location", "", "your cluster region/zone")
	flag.StringVar(&cluster, "cluster", "", "your cluster name")
}
