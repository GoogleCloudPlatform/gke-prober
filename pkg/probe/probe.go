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

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/metrics"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	AvailableUnknown = "Unknown" // AvailableUnknown signifies am indeterminate probe result
	AvailableTrue    = "True"    // AvailableTrue signifies a result where the probe target is available
	AvailableFalse   = "False"   // AvailableFalse signifies a result where the probe target is unavailable
	AvailableError   = "Error"   // AvailableError signifies a result where the probe resulted in an error
)

var (
	defaultProbeResult = ProbeResult{Available: AvailableUnknown}
	resultSuccessful   = ProbeResult{Available: AvailableTrue}
)

type Probe interface {
	Run(context.Context, *v1.Pod, common.Addon) ProbeResult
}

type ProbeMap map[string]Probe

type ProbeResult struct {
	Available string
	Err       error
}

func Run(ctx context.Context, probes ProbeMap, pod *v1.Pod, addon common.Addon) ProbeResult {
	result := defaultProbeResult
	if pr, ok := probes[addon.Name]; ok {
		result = pr.Run(ctx, pod, addon)
		if result.Err != nil {
			klog.Warningf("addon probe %q returned error %v\n", addon.Name, result.Err)
		}
	}
	return result
}

type ConnectivityProbeMap map[string]ConnectivityProbe

type ConnectivityProbe interface {
	Run(context.Context, metrics.ProbeRecorder) error
}
