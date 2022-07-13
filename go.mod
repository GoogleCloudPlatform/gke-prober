module github.com/GoogleCloudPlatform/gke-prober

go 1.16

require (
	cloud.google.com/go v0.97.0
	cloud.google.com/go/monitoring v1.1.0
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v1.0.0-RC1.0.20220121174109-24420159f1b4
	github.com/golang/protobuf v1.5.2
	github.com/googleapis/gax-go/v2 v2.1.1
	github.com/prometheus/client_golang v1.10.0
	go.opentelemetry.io/otel v1.3.0
	go.opentelemetry.io/otel/metric v0.26.0
	go.opentelemetry.io/otel/sdk v1.3.0
	go.opentelemetry.io/otel/sdk/metric v0.26.0
	google.golang.org/api v0.58.0
	google.golang.org/genproto v0.0.0-20211018162055-cf77aa76bad2
	google.golang.org/grpc v1.40.0
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/client-go v0.21.0
	k8s.io/klog/v2 v2.40.1
)
