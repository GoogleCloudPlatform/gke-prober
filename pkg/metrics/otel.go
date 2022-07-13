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

package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/metric/sdkapi"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/api/option"
)

const (
	// MetricNamespace is the name of the namespace used by opentelemetry.
	MetricNamespace = "dummy.net/gke-prober"
)

/*
WARNING: otel provider is not in a working state.

The current version of the otel packages for go do not support
a synchronous gauge or any mechanism to emulate such functionality.
*/
type otelProvider struct {
}

// getPusher returns a Cloud Monitoring metrics pusher
// or a stdout pusher if the DEBUG env var is set to "true"
func StartOTel(ctx context.Context, cfg common.Config) (*otelProvider, error) {
	// Prepare "monitored resource" attributes for Cloud Monitoring
	// (see https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/24420159f1b4bc16579462847eae74b00227b534/exporter/metric/metric.go#L351)
	// k8s_cluster requires location, cluster name
	// k8s_node requires location, cluster name, node name
	attrs := []attribute.KeyValue{
		attribute.String(mexporter.CloudKeyProvider, mexporter.CloudProviderGCP),
		attribute.String(mexporter.CloudKeyZone, cfg.Location), // this is called "zone" but represents location
		attribute.String(mexporter.K8SKeyClusterName, cfg.Cluster),
	}
	if cfg.NodeName != "" {
		attrs = append(attrs, attribute.String(mexporter.HostKeyName, cfg.NodeName))
	}

	popts := []controller.Option{
		controller.WithResource(resource.NewSchemaless(attrs...)),
	}

	// if os.Getenv("DEBUG") == "true" {
	// 	opts := []stdout.Option{
	// 		stdout.WithPrettyPrint(),
	// 	}
	// 	_, pusher, err = stdout.InstallNewPipeline(opts, popts)
	// 	return pusher, err
	// }

	formatter := func(d *sdkapi.Descriptor) string {
		return fmt.Sprintf("%s/%s", common.MetricPrefix, d.Name())
	}
	// Initialize exporter option.
	opts := []mexporter.Option{
		mexporter.WithProjectID(cfg.ProjectID),
		mexporter.WithInterval(cfg.ReportInterval),
		mexporter.WithMetricDescriptorTypeFormatter(formatter),
		mexporter.WithMonitoringClientOptions(
			option.WithUserAgent(cfg.UserAgent),
		),
	}
	pusher, err := mexporter.InstallNewPipeline(opts, popts...)
	if err != nil {
		go func() {
			<-ctx.Done()
			pusher.Stop(context.Background())
		}()
	}

	return &otelProvider{}, err
}

type otelClusterRecorder struct {
	//nodeReadyRecorder     *metric.Int64Counter
	nodeConditionRecorder *metric.Int64Counter
	addonExpectedRecorder *metric.Int64Counter
	nodeAvailableRecorder *metric.Int64Counter
}

func (*otelProvider) ClusterRecorder() ClusterRecorder {
	meter := global.Meter(MetricNamespace)
	nodeConditionRecorder := metric.Must(meter).NewInt64Counter("cluster/node_condition")
	addonExpectedRecorder := metric.Must(meter).NewInt64Counter("cluster/addons_expected")
	nodeAvailableRecorder := metric.Must(meter).NewInt64Counter("cluster/node_available")

	return &otelClusterRecorder{
		nodeConditionRecorder: &nodeConditionRecorder,
		addonExpectedRecorder: &addonExpectedRecorder,
		nodeAvailableRecorder: &nodeAvailableRecorder,
	}
}

func (m *otelClusterRecorder) RecordNodeConditions(counts []LabelCount) {
	ctx := context.Background()
	for _, c := range counts {
		attrs := attributesFromMap(c.Labels)
		m.nodeConditionRecorder.Add(ctx, int64(c.Count), attrs...)
	}
}

func (m *otelClusterRecorder) RecordAddonCounts(counts []LabelCount) {
	ctx := context.Background()
	for _, c := range counts {
		attrs := attributesFromMap(c.Labels)
		m.addonExpectedRecorder.Add(ctx, int64(c.Count), attrs...)
	}
}

func (m *otelClusterRecorder) RecordNodeAvailabilities(counts []LabelCount) {
	ctx := context.Background()
	for _, c := range counts {
		attrs := attributesFromMap(c.Labels)
		m.nodeAvailableRecorder.Add(ctx, int64(c.Count), attrs...)
	}
}

type otelNodeRecorder struct {
	addonCounter                *metric.Int64Counter
	addonCPAvailabilityRecorder *metric.Int64Counter
	nodeConditionRecorder       *metric.Int64Counter
	nodeAvailabilityRecorder    *metric.Int64Counter
	restartCounter              *metric.Int64Counter
}

func (*otelProvider) NodeRecorder() NodeRecorder {
	meter := global.Meter(MetricNamespace)
	addonCounter := metric.Must(meter).NewInt64Counter(
		"addon/available",
		metric.WithDescription("represents a single addon with attributes"),
		// metric.WithUnit("one addon"),
	)
	addonCPAvailabilityRecorder := metric.Must(meter).NewInt64Counter("addon/control_plane_available")
	nodeConditionRecorder := metric.Must(meter).NewInt64Counter("node/node_condition")
	nodeAvailabilityRecorder := metric.Must(meter).NewInt64Counter("node/available")
	restartCounter := metric.Must(meter).NewInt64Counter("addon/restart")

	return &otelNodeRecorder{
		addonCounter:                &addonCounter,
		addonCPAvailabilityRecorder: &addonCPAvailabilityRecorder,
		nodeConditionRecorder:       &nodeConditionRecorder,
		nodeAvailabilityRecorder:    &nodeAvailabilityRecorder,
		restartCounter:              &restartCounter,
	}
}

func (m *otelNodeRecorder) RecordAddonAvailabilies(counts []LabelCount) {
	ctx := context.Background()
	for _, c := range counts {
		attrs := attributesFromMap(c.Labels)
		m.addonCounter.Add(ctx, int64(c.Count), attrs...)
	}
}

func (m *otelNodeRecorder) RecordAddonControlPlaneAvailability(labels map[string]string) {
	ctx := context.Background()
	attrs := attributesFromMap(labels)
	m.addonCPAvailabilityRecorder.Add(ctx, 1, attrs...)
}

func addonAttributes(addon common.Addon) []attribute.KeyValue {
	addonKey := attribute.Key("addon")
	versionKey := attribute.Key("version")
	controllerKey := attribute.Key("controller")
	return []attribute.KeyValue{
		addonKey.String(addon.Name),
		versionKey.String(addon.Version),
		controllerKey.String(addon.Kind),
	}
}

func (m *otelNodeRecorder) RecordNodeConditions(clabels []map[string]string) {
	ctx := context.Background()
	for _, labels := range clabels {
		attrs := attributesFromMap(labels)
		m.nodeConditionRecorder.Add(ctx, 1, attrs...)
	}
}

// formatter specifying capitalized values, aligning with internal k8s style
func boolToStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func (m *otelNodeRecorder) RecordNodeAvailability(labels map[string]string) {
	ctx := context.Background()
	attrs := attributesFromMap(labels)
	m.nodeAvailabilityRecorder.Add(ctx, 1, attrs...)
}

func (m *otelNodeRecorder) RecordContainerRestart(labels map[string]string) {
	ctx := context.Background()
	attrs := attributesFromMap(labels)
	m.restartCounter.Add(ctx, 1, attrs...)
}

type otelProbeRecorder struct {
	httpGetLatencyRecorder   *metric.Int64Histogram
	dnsLookupLatencyRecorder *metric.Int64Histogram
}

func (*otelProvider) ProbeRecorder() ProbeRecorder {
	meter := global.Meter(MetricNamespace)
	httpGetLatencyRecorder := metric.Must(meter).NewInt64Histogram("probe/http_get/request_latency_microseconds")
	dnsLookupLatencyRecorder := metric.Must(meter).NewInt64Histogram("probe/dns_lookup/request_latency_microseconds")

	return &otelProbeRecorder{httpGetLatencyRecorder: &httpGetLatencyRecorder, dnsLookupLatencyRecorder: &dnsLookupLatencyRecorder}
}

func (r *otelProbeRecorder) RecordDNSLookupLatency(elapsed time.Duration) {
	ctx := context.Background()
	r.dnsLookupLatencyRecorder.Record(ctx, elapsed.Microseconds())
}

func (r *otelProbeRecorder) RecordHTTPGetLatency(statusCode int, elapsed time.Duration) {
	ctx := context.Background()
	attributes := []attribute.KeyValue{
		attribute.String("code", fmt.Sprint(statusCode)),
	}
	r.httpGetLatencyRecorder.Record(ctx, elapsed.Microseconds(), attributes...)
}

func attributesFromMap(m map[string]string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	for k, v := range m {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}
