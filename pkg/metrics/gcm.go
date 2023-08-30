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
	"log"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	googlepb "github.com/golang/protobuf/ptypes/timestamp"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	"google.golang.org/grpc/codes"
)

const (
	createTimeSeriesDeadline = 55 * time.Second
)

type gcmProvider struct {
	client   *monitoring.MetricClient
	project  string
	resource *monitoredrespb.MonitoredResource
}

// StartGCM returns a Cloud Monitoring metrics provider
func StartGCM(ctx context.Context, cfg common.Config) (*gcmProvider, error) {
	client, err := monitoring.NewMetricClient(ctx, option.WithUserAgent(common.UserAgent))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	client.CallOptions = &monitoring.MetricCallOptions{
		CreateTimeSeries: []gax.CallOption{
			gax.WithRetry(func() gax.Retryer {
				return gax.OnCodes([]codes.Code{
					codes.DeadlineExceeded,
					codes.Unavailable,
				}, gax.Backoff{
					Initial:    time.Second,
					Max:        16 * time.Second,
					Multiplier: 2,
				})
			}),
		},
	}

	// Prepare metadata to specify the GCM "monitored resource"
	var resource *monitoredrespb.MonitoredResource
	if cfg.Mode == common.ModeCluster {
		resource = &monitoredrespb.MonitoredResource{
			Type: "k8s_cluster",
			Labels: map[string]string{
				"project_id":   cfg.ProjectID,
				"location":     cfg.Location,
				"cluster_name": cfg.Cluster,
			},
		}
	} else {
		resource = &monitoredrespb.MonitoredResource{
			Type: "k8s_node",
			Labels: map[string]string{
				"project_id":   cfg.ProjectID,
				"location":     cfg.Location,
				"cluster_name": cfg.Cluster,
				"node_name":    cfg.NodeName,
			},
		}
	}

	provider := &gcmProvider{
		// Prefer not to store context in a struct type, instead it should be passed as argument
		// ctx:      ctx,
		client:   client,
		project:  cfg.ProjectID,
		resource: resource,
	}

	// A Goroutine usually runs in the backgroud and will close the connection with the CM remote server.
	go func() {
		<-ctx.Done()
		client.Close()
	}()

	return provider, err
}

func gcmIntCounterPoint(value int) *monitoringpb.Point {
	end := time.Now()
	return &monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{
			StartTime: &googlepb.Timestamp{
				Seconds: end.Add(-time.Second).Unix(),
			},
			EndTime: &googlepb.Timestamp{
				Seconds: end.Unix(),
			},
		},
		Value: &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: int64(value),
			},
		},
	}
}

func gcmIntGaugePoint(value int) *monitoringpb.Point {
	return &monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{
			EndTime: &googlepb.Timestamp{
				Seconds: time.Now().Unix(),
			},
		},
		Value: &monitoringpb.TypedValue{
			Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: int64(value),
			},
		},
	}
}

func (p *gcmProvider) intCounterTimeSeries(prefix string, labels map[string]string, value int) *monitoringpb.TimeSeries {
	return &monitoringpb.TimeSeries{
		Metric:     p.metricWithLabels(prefix, labels),
		MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
		Resource:   p.resource,
		Points: []*monitoringpb.Point{
			gcmIntCounterPoint(value),
		},
	}
}

func (p *gcmProvider) intGaugeTimeSeries(prefix string, labels map[string]string, value int) *monitoringpb.TimeSeries {
	return &monitoringpb.TimeSeries{
		Metric:     p.metricWithLabels(prefix, labels),
		MetricKind: metricpb.MetricDescriptor_GAUGE,
		Resource:   p.resource,
		Points: []*monitoringpb.Point{
			gcmIntGaugePoint(value),
		},
	}
}

func (p *gcmProvider) writeTimeSeries(ctx context.Context, ts ...*monitoringpb.TimeSeries) error {
	cctx, cancel := context.WithDeadline(ctx, time.Now().Add(createTimeSeriesDeadline))
	defer cancel()

	err := p.client.CreateTimeSeries(cctx, &monitoringpb.CreateTimeSeriesRequest{
		Name:       fmt.Sprintf("projects/%s", p.project),
		TimeSeries: ts,
	})
	return err
}

func (p *gcmProvider) metricWithLabels(m string, labels map[string]string) *metricpb.Metric {
	return &metricpb.Metric{
		Type:   fmt.Sprintf("%s/%s", common.MetricPrefix, m),
		Labels: labels,
	}
}

func (p *gcmProvider) metric(m string) *metricpb.Metric {
	return p.metricWithLabels(m, map[string]string{})
}

type gcmClusterRecorder struct {
	p *gcmProvider
}

func (p *gcmProvider) ClusterRecorder() ClusterRecorder {
	return &gcmClusterRecorder{p: p}
}

func (r *gcmClusterRecorder) RecordNodeConditions(ctx context.Context, counts []LabelCount) {
	ts := []*monitoringpb.TimeSeries{}
	for _, c := range counts {
		ts = append(ts, r.p.intGaugeTimeSeries(MetricClusterNodeCondition, c.Labels, c.Count))
	}
	r.p.writeTimeSeries(ctx, ts...)
}

func (r *gcmClusterRecorder) RecordAddonCounts(ctx context.Context, counts []LabelCount) {
	ts := []*monitoringpb.TimeSeries{}
	for _, c := range counts {
		ts = append(ts, r.p.intGaugeTimeSeries(MetricClusterAddonsExpected, c.Labels, c.Count))
	}
	r.p.writeTimeSeries(ctx, ts...)
}

func (r *gcmClusterRecorder) RecordNodeAvailabilities(ctx context.Context, counts []LabelCount) {
	ts := []*monitoringpb.TimeSeries{}
	for _, c := range counts {
		ts = append(ts, r.p.intGaugeTimeSeries(MetricClusterNodeAvailable, c.Labels, c.Count))
	}
	r.p.writeTimeSeries(ctx, ts...)
}

type gcmNodeRecorder struct {
	p *gcmProvider
}

func (p *gcmProvider) NodeRecorder() NodeRecorder {
	return &gcmNodeRecorder{p: p}
}

func (r *gcmNodeRecorder) RecordAddonAvailabilies(ctx context.Context, counts []LabelCount) {
	ts := []*monitoringpb.TimeSeries{}
	for _, c := range counts {
		ts = append(ts, r.p.intGaugeTimeSeries(MetricAddonAvailable, c.Labels, c.Count))
	}
	r.p.writeTimeSeries(ctx, ts...)
}

func (r *gcmNodeRecorder) RecordContainerRestart(ctx context.Context, labels map[string]string) {
	ts := r.p.intCounterTimeSeries(MetricAddonRestart, labels, 1)
	r.p.writeTimeSeries(ctx, ts)
}

func (r *gcmNodeRecorder) RecordAddonControlPlaneAvailability(ctx context.Context, labels map[string]string) {
	ts := r.p.intGaugeTimeSeries(MetricAddonCPAvailable, labels, 1)
	r.p.writeTimeSeries(ctx, ts)
}

func (r *gcmNodeRecorder) RecordNodeConditions(ctx context.Context, labels []map[string]string) {
	ts := []*monitoringpb.TimeSeries{}
	for _, clabel := range labels {
		ts = append(ts, r.p.intGaugeTimeSeries(MetricNodeCondition, clabel, 1))
	}
	r.p.writeTimeSeries(ctx, ts...)
}

func (r *gcmNodeRecorder) RecordNodeAvailability(ctx context.Context, labels map[string]string) {
	ts := r.p.intGaugeTimeSeries(MetricNodeAvailable, labels, 1)
	r.p.writeTimeSeries(ctx, ts)
}

type gcmProbeReporter struct {
	p *gcmProvider
}

func (p *gcmProvider) ProbeRecorder() ProbeRecorder {
	return &gcmProbeReporter{p: p}
}

func (r *gcmProbeReporter) RecordDNSLookupLatency(elapsed time.Duration)               {}
func (r *gcmProbeReporter) RecordHTTPGetLatency(statusCode int, elapsed time.Duration) {}
func (r *gcmProbeReporter) RecordAddonHealth(ctx context.Context, labels []map[string]string) {
	ts := []*monitoringpb.TimeSeries{}
	for _, clable := range labels {
		ts = append(ts, r.p.intGaugeTimeSeries(MetricClusterAddonCondition, clable, 1))
	}
	r.p.writeTimeSeries(ctx, ts...)
}

func addonMap(addon common.Addon) map[string]string {
	return map[string]string{
		"addon":      addon.Name,
		"version":    addon.Version,
		"controller": addon.Kind,
	}
}
