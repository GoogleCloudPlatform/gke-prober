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
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
)

// A HTTP server that hanldes Readiness and Liveness probe
// You choose between this built-in server or your own custom server
type Server struct {
	httpserver *http.Server

	clientSet *kubernetes.Clientset

	cw k8s.ClusterWatcher
	cr metrics.ClusterRecorder
	pr metrics.ProbeRecorder
	nw k8s.NodeWatcher
	nr metrics.NodeRecorder

	Config common.Config

	// tickStatusMux protects tick fields
	tickStatusMux sync.RWMutex
	// tickLastStart is equal to start time of last unfinished tick
	tickLastStart time.Time
}

// New return a http server instnace
func NewServer(lchecks *Checks, rchecks *Checks) *Server {

	s := &Server{
		Config: common.GetConfig(),
	}

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

func (s *Server) RunUntil(ctx context.Context, wg *sync.WaitGroup, kubeconfig string) error {

	defer wg.Done()

	go s.graceshutdown(ctx)
	klog.Infof("starting the server with config: %+v\n", s.Config)

	s.clientSet = k8s.ClientOrDie(kubeconfig)
	provider, err := metrics.StartGCM(ctx, s.Config)
	if err != nil {
		panic(err.Error())
	}

	if s.Config.Mode == common.ModeCluster {
		s.cr = provider.ClusterRecorder()
		s.cw = k8s.NewClusterWatcher(s.clientSet)
		// start informers
		s.cw.StartClusterWatches(ctx.Done())
		if s.Config.ClusterProbes {
			s.pr = provider.ProbeRecorder()
		}
		go s.runScrape(ctx)
	} else {
		s.nr = provider.NodeRecorder()
		s.nw = k8s.NewNodeWatcher(s.clientSet, s.Config.NodeName)
		s.nw.StartNodeWatches(ctx.Done())
		go s.runScrape(ctx)
	}

	klog.V(1).Infof("[gke-prober Livenes and Readiness Server]: Lisenting on %s \n", s.httpserver.Addr)
	return s.httpserver.ListenAndServe()
}

func (s *Server) runScrape(ctx context.Context) {
	ticker := time.NewTicker(s.Config.ReportInterval)
	defer ticker.Stop()
	s.tick(ctx, time.Now())

	for {
		select {
		case startTime := <-ticker.C:
			s.tick(ctx, startTime)
		case <-ctx.Done():
			return
		}
	}
}

// Every tick trigges one metrics scraping cycle
// Scrping should complete within the interval, or time out
func (s *Server) tick(ctx context.Context, startTime time.Time) {
	s.tickStatusMux.Lock()
	s.tickLastStart = startTime
	s.tickStatusMux.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.Config.ReportInterval)
	defer cancel()

	klog.V(1).Infof("[%s] Scraping metrics starts\n", startTime.Format(time.RFC3339))
	if common.ModeCluster == s.Config.Mode {
		scheduler.RecordClusterMetrics(ctx, s.cr, s.cw.GetNodes(), s.cw.GetDaemonSets(), s.cw.GetDeployments())
		if s.Config.ClusterProbes {
			scheduler.RecordClusterProbeMetrics(ctx, s.clientSet, s.pr, probe.ClusterProbes())
		}
	} else {
		scheduler.RecordNodeMetrics(ctx, s.nr, s.Config, s.nw.GetNodes(), s.nw.GetPods(), probe.NodeProbes())
	}

	collectTime := time.Since(startTime)
	klog.V(1).Infof("[%s] Scraping cycle ends. Duration(seconds): %f\n", time.Now().Format(time.RFC3339), float64(collectTime)/float64(time.Second))
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

		maxTickWait := time.Duration(1.5 * float64(s.Config.ReportInterval))
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
