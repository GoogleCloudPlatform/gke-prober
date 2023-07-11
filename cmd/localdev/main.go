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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/server"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
)

var (
	project  string
	location string
	cluster  string
)

var (
	kubeconfig string
)

func main() {

	klog.InitFlags(nil)
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

	klog.Infof("starting gke-prober locally with config: %+v\n", cfg)

	ctx := server.SetupSignalContext()
	wg := new(sync.WaitGroup)
	wg.Add(1)

	defer klog.Infof("exiting...")

	s := server.NewServer(nil, nil)
	s.Config = cfg
	s.RunUntil(ctx, wg, kubeconfig)
}

func init() {
	flag.StringVar(&project, "project", "", "your GCP project id")
	flag.StringVar(&location, "location", "", "your cluster region/zone")
	flag.StringVar(&cluster, "cluster", "", "your cluster name")
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}

	klog.InitFlags(nil)
	flag.Parse()
}
