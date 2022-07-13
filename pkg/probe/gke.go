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

package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"cloud.google.com/go/compute/metadata"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	v1 "k8s.io/api/core/v1"
)

func GKEProbes() ProbeMap {
	return ProbeMap{
		"fluentbit":           newFluntdProbe(),
		"gke-metadata-server": newGkeMetadataServerProbe(),
	}
}

func resultFromError(err error) ProbeResult {
	return ProbeResult{
		Available: AvailableError,
		Err:       err,
	}
}

type fluentdProbe struct {
}

func newFluntdProbe() *fluentdProbe {
	return &fluentdProbe{}
}

func (p *fluentdProbe) Run(ctx context.Context, pod *v1.Pod, addon common.Addon) ProbeResult {
	ip := pod.Status.HostIP
	_, err := fetchFluentbitUptime(ip)
	if err != nil {
		err = fmt.Errorf("fetchFluentbitUptime returned unexpected error: %v", err)
		return resultFromError(err)
	}
	return defaultProbeResult
}

func fetchFluentbitUptime(ip string) (string, error) {
	resp, err := http.Get("http://" + ip + ":2020/api/v1/uptime")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

type gkeMetadataServerProbe struct {
}

func newGkeMetadataServerProbe() *gkeMetadataServerProbe {
	return &gkeMetadataServerProbe{}
}

func (p *gkeMetadataServerProbe) Run(ctx context.Context, pod *v1.Pod, addon common.Addon) ProbeResult {
	_, err := metadata.Email("default")
	if err != nil {
		err = fmt.Errorf("metadata.Email(default) returned %v", err)
		return resultFromError(err)
	}
	return resultSuccessful
}
