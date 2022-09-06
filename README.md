# Overview
`gke-prober` runs inside your GKE cluster, gathering and exporting additional metrics not supplied natively by the GKE product. It targets four types of metrics:
1. Metrics that describe the health of your nodes
1. Metrics that describe the health of node-hosted GKE components (addons in the `kube-system` namespace, as annotated by `components.gke.io/component-name`)
1. Metrics collected from probing of the compute environment (e.g. dns, networking)
1. Service level indicators, describing the health of your nodes and node-hosted GKE components in an aggregate fashion

The metrics are labelled with a few other dimensions, as appropriate, related to the metrics themselves.

Note: this is not an officially supported Google product.

# Table of Contents

* [Installation](#installation)
* [Requirements](#requirements)
* [Metrics](#metrics)
* [Development](#development)
* [License](#license)

# Installation

## Container image
Currently, users would need to build its own container image using the provided Dockerfile.
For build tools, we recommend the Google Cloud Build 
```
# go to the root directory where the Dockerfile resides

gcloud builds submit --tag $IMG
```

Manifests in the `manifests` directory specify the GCP resources (as KCC resources) and kubernetes resources needed to run `gke-prober` in GKE clusters with Workload Identity enabled.

Before applying, set your GCP project in all the manifests by hand, or by executing a kpt function at the root of the repository:

```bash
mkdir -p ${HOME}/bin
curl -L https://github.com/GoogleContainerTools/kpt/releases/download/v1.0.0-beta.1/kpt_linux_amd64 --output ~/bin/kpt && chmod u+x ${HOME}/bin/kpt
export PATH=${HOME}/bin:${PATH}

kpt fn eval --truncate-output=false --image gcr.io/kpt-fn/apply-setters:v0.2 manifests -- project=my-gcp-project
```

Kpt requires Docker to run, if you don't have Docker installed, execute the Sed commend at the root of the repository to set your GCP project id in all the Yaml files:

```bash
# replace gcp-project-id with the real project id
find . -type f -name "*.yaml" -print0 | xargs -0 sed -i'' -e 's/my-gcp-project/gcp-project-id/g'
```

Then apply the manifests to your GKE clusters. Manifests in `manifests/gcp` need to be applied into a KCC-enabled cluster. Manifests in `manifests/k8s` should be applied in the clusters where you'd like `gke-prober` installed.
# Requirements

## Kubernetes manifests
`gke-prober` runs in two modes. These modes are complementary: _make sure you run both_.

1. Cluster Mode: Runs as a deployment with a single replica, gathering cluster-level data.
1. Node Mode: Runs as a daemonset, gathering node-level data.

## Required Permissions
### RBAC permissions to the API server.
`gke-prober` has its own Kubernetes service account in the `gke-prober-system` namespace, which requires `list` and `watch` permissions on the following resources:
1. `nodes`
1. `pods`
1. `daemonsets`
1. `deployments`

### IAM permissions for Cloud Monitoring.
`gke-prober` runs under an IAM service account with the following IAM permissions:
1. monitoring.metricDescriptors.create
1. monitoring.timeSeries.create
1. monitoring.metricDescriptors.list (optional, for prom-to-sd sidecar used for golang process metrics)

### Workload Identity
`gke-prober`uses the Kubernetes service account (KSA) within your cluster to authenicate to the cluster API server. It also runs as an IAM service account (GSA) to call Cloud Monitoring APIs to expose the metrics it collects to the Cloud Monitoring/StackDriver remote backend.

If you want to install `gke-prober` on GKE clusters. [GKE Workload Identify](https://cloud.google.com/kubernetes-engine/docs/concepts/workload-identity) is the recommended approach. Workload Identify binds a KSA to a GSA, it is the recommended way for your workloads (e.g. `gke-prober`) running on GKE to access Google Cloud services in a secure and manageable way.

If your GKE cluster is configured with Workload Identity the `gke-prober` service account needs additional permissions, and needs to be linked to the k8s service account. See the [documentation page](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#gcloud) for Workload Identity, and refer to `hack/workload-identity.sh` (setting PROJECT_ID) for a concrete example.

# Metrics
## Dashboard
Once running, `gke-prober` will emit a variety of metrics.

 (work-in-progress!!) Two dashboards are available in  `dashboards/cloud-monitoring`:
 1. `gke-prober-fleet.json` visualizes node and addon metrics at a fleet level (multiple GKE clusters).
 1. `gke-prober-cluster.json` visualizes node and addon metrics at a cluster level: please filter on your GKE cluster name.
 1. `gke-prober-performance.json` visualizes the performance of `gke-prober` itself (memory/cpu usage, Cloud Monitoring API usage). Note: in the "Consumed API" chart, modify the `credential_id` filter to match the ID of your `gke-prober-sa` service account. 
 
 The dashboards look roughly like this (slightly out-of-date screenshot):

![Very rough attempt at visualizing gke-prober metrics](dashboards/cloud-monitoring/gke-prober.png)

## Metric List
`gke-prober` emits the following metrics:

Metric Type | Prober Mode | Implemented | Metric | Source | Description
----------- | ----------- | ----------- | ------ | ------ | -----------
| Addon | Cluster | - [x]           | cluster/addons_expected | apiserver | Expected count of addons, labelled by `addon:$name`, `controller:{DaemonSet,Deployment}` and `version:$version`
| Node  | Cluster | - [x]           | cluster/node_available | apiserver | Count of nodes labelled by `available:{True,False}`, `ready:{True,False}`, `schedulable:{True,False}`, `done_warming:{True,False}`, `nodepool:$nodepool`, and `zone:$zone`. `Available` indicates nodes are `healthy`, `schedulable`, `done_warming`
| Node  | Cluster | - [x]           | cluster/node_condition | apiserver | Count of nodes by NodeCondition, labelled by `nodepool:$nodepool`, `zone:$zone`, `type:$type` and `status:$status`
| Node  | Node    | - [x]           | node/available | apiserver | Node availability, labelled by `available:{True,False}`, `ready:{True,False}`, `schedulable:{True,False}`, `done_warming:{True,False}`, `nodepool:$nodepool`, and `zone:$zone` (value 1).
| Node  | Node    | - [x]           | node/condition | apiserver | Node conditions, labelled by `nodepool:$nodepool`, `zone:$zone`, `type:$type` amd `status:$status` (value 1).
| Addon | Node    | - [x]           | addon/restart | apiserver | Count of addon restarts on node, labelled by `reason:$reason`, `exit_code:$code`, `container_name:$container`, `addon:$name`, `controller:{DaemonSet,Deployment}`, `version:$version`, `nodepool:$nodepool`, and `zone:$zone`
| Addon | Node    | - [x]           | addon/control_plane_available | apiserver/probe | Addon control plane availability (if all addons on node are scheduled, running, and haven't restarted), labelled by `available:{True,False}`, `nodepool:$nodepool`, and `zone:$zone` (value 1)
| Addon | Node    | - [x]           | addon/available | apiserver | Count of addon pods on node, labelled by `addon:$name`, `controller:{DaemonSet,Deployment}`, `version:$version`, `nodepool:$nodepool`, `zone:$zone`, `available:{True,False}`, `node_available:{True,False}`, `running:{True,False}`, and `stable:{True,False}`
| Addon | Node    | - [] partial*   | addon/available | probe | Addon probes will augment this metric with the label `healthy:{True,False,Unknown,Error}`
| Addon | Node    | - [] partial**  | addon/`addon`/* | probe | Addon probes will emit addon-specific metrics, labelled by `addon:$name`, controller:{DaemonSet,Deployment}` and `version:$version`
| Probe | Node    | - [] partial*** | probe/`probe`/* | probe | Probes will emit metrics gathered by probing the compute environment


\* Health probes implemented for `gke-metadata-server`

\*\* Metrics emitted for ...

\*\*\* Probes for `dns-lookup` and `http-get` emit a metric called `request_latency_microseconds`
## SLIs
`gke-prober` will attempt to export metrics that can support the following SLIs:
1. Node Availability (k8s_node: `node/available`)
1. Addon Control Plane Availability (k8s_node: ``)

## Monitored Resource
In Cloud Monitoring, metrics emitted by `gke-prober` will be visible under:
1. the `k8s-cluster` monitored resource when running in cluster mode
1. the `k8s-node` monitored resource when running in node mode
## Probes and Probe Metrics
Addon probes will run from `node` mode, and probe the local pods representing the addon

## Listing and Deleting Metrics in Cloud Monitoring
To list the metrics descriptors that `gke-prober` creates, run:
```
go run cmd/listmd/main.go -project my-gcp-project
```
If you need to delete those metrics descriptors, you can pass `-delete` to the above commandline.

Note that you need to authenticate to Google Cloud, either by using a GCP service account or your user credential
### Hostnetwork Probes
As some addons run in the node's network namespace and expose probe-able endpoints on `localhost`, the prober can only probe those addons when running with `hostNetwork:true`. Examples of those addons include the `node-problem-detector` and `gke-metrics-agent`.

No GKE components requiring `hostNetwork` mode have been implemented as of July 2021.

Note: most GKE addons in this category only export metrics endpoints which may not be a useful (or stable) proxy for health.
# Development

To build, export `IMG=gcr.io/your_project/gke-prober:latest` and `make docker-build docker-push`. Use that IMG in the deployment manifests.

For local development (`make run`), gke-prober will use the k8s cluster in your current kubectl context. To use the stdout OpenTelemetry exporter, use `DEBUG=true` (`DEBUG=true make run`).

For local development exporting to Cloud Monitoring, export `PROJECT_ID=your_project` and make sure you have the requisite permissions in your current GCP session.

Process metrics are exported on `:8080/metrics`

# License
[Apache License 2.0](LICENSE)