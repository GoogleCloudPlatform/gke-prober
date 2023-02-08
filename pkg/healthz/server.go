package healthz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

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
func (s *Server) New(lchecks *Checks, rchecks *Checks, addr string, interval time.Duration) error {

	s.interval = interval
	mux := http.NewServeMux()
	// mux.Handle("/healthz", rchecks.Healthz())

	// The default liveness probe is tickSerivce unless specified
	if lchecks != nil {
		mux.Handle("/liveness", lchecks.Healthz())
	} else {
		mux.Handle("/liveness", s.Healthz())
	}

	s.httpserver = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	return nil
}

func (s *Server) Start(ctx context.Context, wg *sync.WaitGroup) error {

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer wg.Done()

	go s.tick(cctx)
	go s.graceshutdown(cctx)

	klog.Infof("[Livenes and Readiness Server]: Lisenting on %s \n", s.httpserver.Addr)
	if err := s.httpserver.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		klog.Warningf("[Livenes and Readiness Server]: Could not listen on %s, [Reason]: %s", s.httpserver.Addr, err.Error())
	}

	return nil
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
			klog.Infof("Health-check server alive, last tick [%s],", s.tickLastStart.Format(time.RFC3339))
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
		klog.Infof("Liveness check by kubelet, tickWait is %s\n", tickWait.String())
		if !lastTickStart.IsZero() && tickWait > maxTickWait {
			klog.Infof("Failed default Liveness probe, [tickWait]:%d exceeds [maxTickWait]:%d", tickWait, maxTickWait)
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
