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

package server

import (
	"encoding/json"
	"net/http"

	"k8s.io/klog/v2"
)

// Make sure every healthcheck implement Healthz function
type Checkable interface {
	Healthz() error
}

type Error struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Service contains the name of a healthcheck target and its status
type Service struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
}

// Response representing the aggregated healthcheck result
type Response struct {
	Service []Service `json:"services,omitempty"`
	Errors  []Error   `json:"errors,omitempty"`
	Healthy bool      `json:"healthy"`
}

// Target represents the instance of a healthcheck target
type Target struct {
	Handle Checkable
	svc    Service
}

// Checks representing a collection of healthcheck targets
type Checks struct {
	Targets []Target
}

// Healthz returns a http.HandlerFunc and run health check against the targets
func (h *Checks) Healthz() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var errs []Error
		var svcs []Service

		if h.Targets != nil {
			for _, target := range h.Targets {
				if err := target.Handle.Healthz(); err != nil {
					target.svc.Healthy = false
					errs = append(errs, Error{
						Name:    target.svc.Name,
						Message: err.Error(),
					})
				}
				svcs = append(svcs, Service{
					Name:    target.svc.Name,
					Healthy: target.svc.Healthy,
				})
			}
		}

		response := Response{
			Service: svcs,
			Errors:  errs,
			Healthy: true,
		}

		if len(errs) > 0 {
			response.Healthy = false
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json, err := json.Marshal(response)
		if err != nil {
			klog.Warning(err.Error())
		}
		w.Write(json)
	})
}
