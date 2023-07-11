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
	"path/filepath"
	"sync"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/server"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
)

var (
	kubeconfig string
)

func main() {
	ctx := server.SetupSignalContext()
	wg := new(sync.WaitGroup)
	wg.Add(1)

	s := server.NewServer(nil, nil)
	s.RunUntil(ctx, wg, kubeconfig)
}

func init() {
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
	klog.InitFlags(nil)
	flag.Parse()
}
