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
	"net"
	"net/http"
	"time"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/metrics"
)

const (
	dnsLookupHost = "google.com"
	httpGetURL    = "http://googleapis.com/generate_204"
)

func ConnectivityProbes() ConnectivityProbeMap {
	return ConnectivityProbeMap{
		"http_get":   newHTTPGetProbe(),
		"dns_lookup": newDnsLookupProbe(),
	}
}

type httpGetProbe struct {
	url string
}

func newHTTPGetProbe() *httpGetProbe {
	return &httpGetProbe{
		url: httpGetURL,
	}
}

func (p *httpGetProbe) Run(ctx context.Context, pr metrics.ProbeRecorder) error {
	start := time.Now()
	resp, err := http.Get(p.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	pr.RecordHTTPGetLatency(resp.StatusCode, time.Since(start))
	return nil
}

type dnsLookupProbe struct {
	host string
}

func newDnsLookupProbe() *dnsLookupProbe {
	return &dnsLookupProbe{
		host: dnsLookupHost,
	}
}

func (p *dnsLookupProbe) Run(ctx context.Context, pr metrics.ProbeRecorder) error {
	start := time.Now()
	ips, err := net.LookupIP(p.host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IPs returned in dns lookup response")
	}
	pr.RecordDNSLookupLatency(time.Since(start))
	return nil
}
