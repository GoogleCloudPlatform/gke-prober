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

package localcontroller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreV1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const controllerAgentName = "gke-prober-local-controller"

const (
	SuccessSynced = "Synced"
)

type Controller struct {
	kubeclientset kubernetes.Interface
	nodesLister   corelister.NodeLister
	nodesSynced   cache.InformerSynced
	workqueue     workqueue.RateLimitingInterface
}

func NewController(
	kubeclientset kubernetes.Interface,
	nodesInformer coreV1informers.NodeInformer) *Controller {

	// Create a new controller
	klog.Info("Creating a local controller to manage node-level probers")
	controller := &Controller{
		kubeclientset: kubeclientset,
		nodesLister:   nodesInformer.Lister(),
		nodesSynced:   nodesInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodeworkers"),
	}

	klog.Info("Setting up event handlers")
	nodesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newNode := new.(*corev1.Node)
			oldNode := old.(*corev1.Node)
			if newNode.ResourceVersion == oldNode.ResourceVersion {
				// Periodic resync will send update events for all known node.
				// Two different versions of the same Node will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	return controller
}

// Run start the controller, setting up the event handler, as well as
// syncing informer internal caches and starting workers.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	if ok := cache.WaitForCacheSync(stopCh, c.nodesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	// Start n workers in parallel
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, 5*time.Second, stopCh)
	}

	<-stopCh
	return nil
}

func (c *Controller) runWorker() {
	for c.processNextQueueItem() {
	}
}

// processNextQueueItem reads a single work time off the queue
// and attempt to process it
func (c *Controller) processNextQueueItem() bool {
	return true
}

// Here it includes the actual business logic to
// start the node prober when a new node is added or
// shutdown the prober when a node is deleted
func (c *Controller) syncHandler(key string) error {
	fmt.Printf("SyncHandler called to handle %s\n", key)
	return nil
}

// handleObject runs filtering on the events and
// it only inserts the events we need to care in the workqueue
func (c *Controller) handleObject(obj interface{}) {
	fmt.Println("Detected Node changes!")
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		deleted, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = deleted.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding delete object, invalid type"))
			return
		}
	}
	c.enqueue(object)
}

func (c *Controller) enqueue(obj interface{}) {
	var key string
	var err error

	// Convert the object into a key (in this case
	// we are doing it in the format of 'namespace/name')
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func StartController(ctx context.Context, kubeclientset *kubernetes.Clientset) {
	stopCh := make(chan struct{})

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclientset, time.Second*30)

	controller := NewController(kubeclientset,
		kubeInformerFactory.Core().V1().Nodes())

	kubeInformerFactory.Start(stopCh)

	if err := controller.Run(1, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
		return
	}

	select {
	case <-ctx.Done():
		close(stopCh)
	}
}
