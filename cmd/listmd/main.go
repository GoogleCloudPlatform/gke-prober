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
	"strings"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"google.golang.org/api/iterator"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

var metricPrefix = common.MetricPrefix

var (
	ignorePaths = []string{"go", "process", "promhttp"}
)

func main() {
	project := flag.String("project", "", "your GCP project id")
	delete := flag.Bool("delete", false, "set to remove all gke-prober metric descriptors")
	flag.Parse()

	if *project == "" {
		fmt.Println("Please supply a project id.")
		os.Exit(1)
	}

	if err := listMetrics(*project, *delete); err != nil {
		fmt.Printf("err: %v\n", err)
	}
}

// listMetrics lists all the metrics available to be monitored in the API.
func listMetrics(projectID string, delete bool) error {
	ctx := context.Background()
	c, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	deleteDescriptor := func(d *metricpb.MetricDescriptor) error {
		req := &monitoringpb.DeleteMetricDescriptorRequest{
			Name: "projects/" + projectID + "/metricDescriptors/" + d.GetType(),
		}
		return c.DeleteMetricDescriptor(ctx, req)
	}

	req := &monitoringpb.ListMetricDescriptorsRequest{
		Name: "projects/" + projectID,
	}
	iter := c.ListMetricDescriptors(ctx, req)

	for {
		resp, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("Could not list metrics: %v", err)
		}
		m := resp.GetType()
		switch {
		case !strings.HasPrefix(m, metricPrefix):
			continue
		case strings.HasPrefix(m, metricPrefix+ignorePaths[0]),
			strings.HasPrefix(m, metricPrefix+ignorePaths[1]),
			strings.HasPrefix(m, metricPrefix+ignorePaths[2]):
			continue
		}
		if delete {
			fmt.Printf("Deleting %q: ", resp.GetType())
			err := deleteDescriptor(resp)
			if err == nil {
				fmt.Println("success")
			} else {
				fmt.Println("error: %w", err)
			}
		} else {
			fmt.Printf("%v (%v)\n", resp.GetType(), resp.GetMetricKind())
		}
	}
	return nil
}
