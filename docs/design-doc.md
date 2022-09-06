# Objective
Deliver a prober that GKE customers can choose to run inside their GKE clusters, surfacing:
1. Cluster/node metrics not otherwise exposed in Google Cloud Monitoring
1. Specific metrics about the set of semi-managed GKE addons running on the customer nodes

# Architecture


# Implementation details

## Metrics Pipeline
`gke-prober` currently supports two types of metrics providers: 
1. Google Cloud Monitoring (formaly Stackdriver)
1. Open-Telemetry 

By default, the prober uses the Cloud Monitoring APIs to expose metrics. Users can use metrics explorer to view metrics in Cloud monitoring.
You can also choose to implement your own providers. A metrics provider is defined as a Go interface which supports the following functions and/or methods

```go
type Provider interface {
	ClusterRecorder() ClusterRecorder
	NodeRecorder() NodeRecorder
	ProbeRecorder() ProbeRecorder
}
```

A provider needs to implement 3 different "Recorder". Each recorder supports a variety of functions to montior cluster/node level components and emit specific metrics.

## Go Packages and Classes
### main.go
The job of the main goroutine is to load the configurations and then start the metrics provider
```go
// Pre load the configurations from the command line as well as from the OS enviroment variables 
func getConfig()
func getEnv()
func getMetadata()

// Kubeconfig is only needed if you run the prober as an application outside of the cluster. The prober uses the credentials in kubeconfig for authentication
// On cluster, the prober calls "in-cluster-config" to find out the attached service account in order to authenticate to the K8S ApiServer
func getKubeconfig()

// Start the metrics provider and cluster/node recorders
provider, err = metrics.StartGCM(ctx, cfg)
cr := provider.ClusterRecorder()

// Start a set of K8S watcher/informers 
w := k8s.NewClusterWatcher(clientset)
w.StartClusterWatches(ctx)
```

### k8s.go
The k8s package provides a set of wrappers for functions in client-go to handle the communication with your cluster
```go
// Create a clientSet to work with different of API groups and versions
func ClientOrDie(kubeconfig string) *kubernetes.Clientset {
	kConfig, err := rest.InClusterConfig()
	clientset, err := kubernetes.NewForConfig(kConfig)
}

// A watcher is a collection of client-go informers created from the informer shared factory
type clusterWatcher struct {
	DaemonSetInformer  cache.SharedInformer
	DeploymentInformer cache.SharedInformer
	NodeInformer       cache.SharedInformer
}

type nodeWatcher struct {
	NodeInformer            cache.SharedInformer
	PodInformer             cache.SharedInformer
	containerRestartHandler func(pod *v1.Pod, status v1.ContainerStatus)
}

// Each informer runs in a separate goroutine
func (w *clusterWatcher) StartClusterWatches(ctx context.Context) {
	go w.DaemonSetInformer.Run(stop)
	go w.DeploymentInformer.Run(stop)
	go w.NodeInformer.Run(stop)
}

```

### scheduler.go
scheduler implements a control loop that runs in the main goroutine. 

The loop runs with a timer. On timeout, metrics are being generated from the addons under monitoring and get reported to Cloud Monitoring

```go
// A timer that controls the metrics reporting
func StartClusterRecorder(ctx context.Context, recorder metrics.ClusterRecorder, watcher k8s.ClusterWatcher, interval time.Duration) {
	t := time.NewTicker(interval)
	for {
		select {
		case <-t.C:
			recordClusterMetrics(recorder, watcher.GetNodes(), watcher.GetDaemonSets(), watcher.GetDeployments())
		case <-ctx.Done():
			return
		}
	}
}

```

### metrics.go
metrics package includes the metrics metadata based on the format defined in Google Cloud Monitoring, including metric types and lables.
See [metrics structure](*https://cloud.google.com/monitoring/api/v3/metrics-details)

### gcm.go
gcm package is the Cloud monitoring implementation for the provider interface. It uses Google Cloud monitoring client library and calls APIs to generate time series and expose metrics
```go
// StartGCM returns a Cloud Monitoring client
func StartGCM(ctx context.Context, cfg common.Config) (*gcmProvider, error) {
	client, err := monitoring.NewMetricClient(ctx, option.WithUserAgent(common.UserAgent))
}

// Call Cloud Monitoring API to emit metrics
func (p *gcmProvider) writeTimeSeries(ts ...*monitoringpb.TimeSeries)
```

## Goroutines
Besides the main goroutine, the prober spawns a number of goroutines for specific purposes respectively:

1. K8S Informers. Each watches for changes in a specific resource type
```go
func (w *clusterWatcher) StartClusterWatches(ctx context.Context) {
	go w.DaemonSetInformer.Run(stop)
	go w.DeploymentInformer.Run(stop)
	go w.NodeInformer.Run(stop)
}

func (w *nodeWatcher) StartNodeWatches() {
	go w.NodeInformer.Run(stop)
	go w.PodInformer.Run(stop)
}
```
2. Cluster/Node recorders. The main job of the recorders is to emit data points and expose metrics using Cloud Monitoring APIs
```go
go scheduler.StartClusterRecorder(ctx, cr, w, cfg.ReportInterval)
go s.StartReporting(ctx, w, probe.GKEProbes(), cfg.ReportInterval)
```
3. Prometues
```go
go http.ListenAndServe(localPromPort, nil)
```

### Context
Synchronization across all goroutines is controlled by a single go context with cancel method. The context is created in the main goroutine which goes into blocked after successfully spawning all worker goroutings
```go
// Create a context without timeout
ctx, cancel := context.WithCancel(context.Background())

// The main goroutine goes into blocked until after being interrupted by Ctrl+c
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

// Cancel all worker goroutines and main exits
cancel()
```

## Client-go
`gke-prober`leverages K8S informers to watch resource status in your GKE clusters. Informers are built on top of client-go library. 

## Future Improvements
1. For goroutine synchronization, shall we use sync.waitgroup in additon to the context to make sure the main exits until after all work routines clean up their ongoing works