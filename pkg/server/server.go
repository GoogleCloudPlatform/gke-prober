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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/gke-prober/pkg/common"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/k8s"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/metrics"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/probe"
	"github.com/GoogleCloudPlatform/gke-prober/pkg/scheduler"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
)

// A HTTP server that hanldes Readiness and Liveness probe
// You choose between this built-in server or your own custom server
type Server struct {
	httpserver *http.Server
	interval   time.Duration

	// tickStatusMux protects tick fields
	tickStatusMux sync.RWMutex
	// tickLastStart is equal to start time of last unfinished tick
	tickLastStart time.Time
}

// New return a http server instnace
func NewServer(lchecks *Checks, rchecks *Checks, interval time.Duration) *Server {

	s := &Server{}
	s.interval = interval
	mux := http.NewServeMux()

	// Register probers (liveness and Readiness)
	// The default liveness probe is tickSerivce unless specified
	if lchecks != nil {
		mux.Handle("/liveness", lchecks.Healthz())
	} else {
		mux.Handle("/liveness", s.Healthz())
	}

	s.httpserver = &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	return s
}

func (s *Server) RunUntil(ctx context.Context, wg *sync.WaitGroup, kubeconfig string) {

	defer wg.Done()

	go s.tick(ctx)
	go s.graceshutdown(ctx)

	cfg := common.GetConfig()
	klog.Infof("starting the server with config: %+v\n", cfg)

	clientset := k8s.ClientOrDie(kubeconfig)
	provider, err := metrics.StartGCM(ctx, cfg)
	if err != nil {
		panic(err.Error())
	}

	if cfg.Mode == common.ModeCluster {
		cr := provider.ClusterRecorder()
		w := k8s.NewClusterWatcher(clientset)
		w.StartClusterWatches(ctx)
		go scheduler.StartClusterRecorder(ctx, cr, w, cfg.ReportInterval)
		if cfg.ClusterProbes {
			pr := provider.ProbeRecorder()
			go scheduler.StartClusterProbes(ctx, clientset, pr, probe.ClusterProbes(), cfg.ReportInterval)
		}
	} else {
		nr := provider.NodeRecorder()
		s := scheduler.NewNodeScheduler(nr, cfg)
		w := k8s.NewNodeWatcher(clientset, cfg.NodeName)
		w.StartNodeWatches(ctx, s.ContainerRestartHandler(ctx))
		go s.StartReporting(ctx, w, probe.NodeProbes(), cfg.ReportInterval)
	}

	klog.Infof("[Livenes and Readiness Server]: Lisenting on %s \n", s.httpserver.Addr)
	if err := s.httpserver.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Warningf("[Livenes and Readiness Server]: Could not listen on %s, [Reason]: %s", s.httpserver.Addr, err.Error())
	}
}

func (s *Server) graceshutdown(ctx context.Context) {
	<-ctx.Done()

	newctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.httpserver.SetKeepAlivesEnabled(false)
	if err := s.httpserver.Shutdown(newctx); err != nil {
		klog.Warningf("Health Check Server failed to shutdown due to [%s]", err.Error())
	}
}

func (s *Server) tick(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			s.tickStatusMux.Lock()
			s.tickLastStart = time.Now()
			klog.V(1).Infof("Health-check server alive, last tick [%s],", s.tickLastStart.Format(time.RFC3339))
			s.tickStatusMux.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) Healthz() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		errs := []Error{}
		svcs := []Service{}

		svcs = append(svcs, Service{
			Name:    "default liveness check",
			Healthy: true,
		})

		s.tickStatusMux.RLock()
		lastTickStart := s.tickLastStart
		s.tickStatusMux.RUnlock()

		maxTickWait := time.Duration(1.5 * float64(s.interval))
		tickWait := time.Since(lastTickStart)
		klog.V(2).Infof("Liveness check by kubelet, tickWait is %s\n", tickWait.String())
		if !lastTickStart.IsZero() && tickWait > maxTickWait {
			klog.Warningf("Failed default Liveness probe, [tickWait]:%d exceeds [maxTickWait]:%d", tickWait, maxTickWait)
			err := fmt.Sprint("Tick not finished on time")
			errs = append(errs, Error{
				Name:    "Default liveness probe",
				Message: err,
			})
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
