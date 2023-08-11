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

package k8s

import (
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func ClientOrDie(kubeconfig string) *kubernetes.Clientset {
	// try in-cluster config, and then default to kubeconfig
	kConfig, err := rest.InClusterConfig()
	if err != nil {
		// use the current context in kubeconfig
		kConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}
	clientset, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

type NodeWatcher interface {
	GetPods() []*v1.Pod
	GetNodes() []*v1.Node
	StartNodeWatches(<-chan struct{})
}

type nodeWatcher struct {
	NodeInformer cache.SharedInformer
	PodInformer  cache.SharedInformer
}

func NewNodeWatcher(cs *kubernetes.Clientset, nodeName string) NodeWatcher {
	nodeInformer := informers.NewFilteredNodeInformer(cs, 0, cache.Indexers{}, func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("metadata.name", nodeName).String()
	})
	podInformer := informers.NewFilteredPodInformer(cs, metav1.NamespaceSystem, 0, cache.Indexers{}, func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	})

	return &nodeWatcher{
		NodeInformer: nodeInformer,
		PodInformer:  podInformer,
	}
}

func (w *nodeWatcher) GetPods() []*v1.Pod {
	if !w.PodInformer.HasSynced() {
		klog.V(1).Infoln("cache for pod informer has not fully synced")
		return nil
	}
	pods := []*v1.Pod{}
	for _, p := range w.PodInformer.GetStore().List() {
		p := p.(*v1.Pod)
		pods = append(pods, p)
	}
	return pods
}

func (w *nodeWatcher) GetNodes() []*v1.Node {
	if !w.NodeInformer.HasSynced() {
		klog.V(1).Infoln("cache for node informer has not fully synced")
		return nil
	}
	nodes := []*v1.Node{}
	for _, n := range w.NodeInformer.GetStore().List() {
		n := n.(*v1.Node)
		nodes = append(nodes, n)
	}
	return nodes
}

func (w *nodeWatcher) StartNodeWatches(stopCh <-chan struct{}) {
	go w.NodeInformer.Run(stopCh)
	go w.PodInformer.Run(stopCh)
}

type ClusterWatcher interface {
	GetDaemonSets() []*appsv1.DaemonSet
	GetDeployments() []*appsv1.Deployment
	GetNodes() []*v1.Node
	StartClusterWatches(<-chan struct{})
}

type clusterWatcher struct {
	DaemonSetInformer  cache.SharedInformer
	DeploymentInformer cache.SharedInformer
	NodeInformer       cache.SharedInformer
}

func NewClusterWatcher(cs *kubernetes.Clientset) ClusterWatcher {
	daemonSetInformer := appsinformers.NewDaemonSetInformer(cs, metav1.NamespaceSystem, 0, cache.Indexers{})
	deploymentInformer := appsinformers.NewDeploymentInformer(cs, metav1.NamespaceSystem, 0, cache.Indexers{})
	nodeInformer := informers.NewNodeInformer(cs, 0, cache.Indexers{})

	return &clusterWatcher{
		DaemonSetInformer:  daemonSetInformer,
		DeploymentInformer: deploymentInformer,
		NodeInformer:       nodeInformer,
	}
}

func (w *clusterWatcher) StartClusterWatches(stopCh <-chan struct{}) {
	// stop := make(chan struct{})
	go w.DaemonSetInformer.Run(stopCh)
	go w.DeploymentInformer.Run(stopCh)
	go w.NodeInformer.Run(stopCh)
}

func (w *clusterWatcher) GetDaemonSets() []*appsv1.DaemonSet {
	if !w.DaemonSetInformer.HasSynced() {
		klog.V(1).Infoln("cache for Daemonsets informer has not fully synced")
		return nil
	}
	daemonSets := []*appsv1.DaemonSet{}
	for _, d := range w.DaemonSetInformer.GetStore().List() {
		d := d.(*appsv1.DaemonSet)
		daemonSets = append(daemonSets, d)
	}
	return daemonSets
}

func (w *clusterWatcher) GetDeployments() []*appsv1.Deployment {
	if !w.DeploymentInformer.HasSynced() {
		klog.V(1).Infoln("cache for deployment informer has not fully synced")
		return nil
	}
	deployments := []*appsv1.Deployment{}
	for _, d := range w.DeploymentInformer.GetStore().List() {
		d := d.(*appsv1.Deployment)
		deployments = append(deployments, d)
	}
	return deployments
}

func (w *clusterWatcher) GetNodes() []*v1.Node {
	if !w.NodeInformer.HasSynced() {
		klog.V(1).Infoln("cache for node informer has not fully synced")
		return nil
	}
	nodes := []*v1.Node{}
	for _, n := range w.NodeInformer.GetStore().List() {
		n := n.(*v1.Node)
		nodes = append(nodes, n)
	}
	return nodes
}
