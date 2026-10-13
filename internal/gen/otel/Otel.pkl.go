// Code generated from Pkl module `alexandria.Otel`. DO NOT EDIT.
package otel

import (
	"context"

	"github.com/apple/pkl-go/pkl"
)

// OpenTelemetry: traces, metrics and logs over OTLP gRPC. On the k3s cluster
// the endpoint is the otel-lgtm service in namespace `monitoring`.
type Otel struct {
	Enabled bool `pkl:"enabled"`

	// OTLP gRPC endpoint (host:port).
	Endpoint string `pkl:"endpoint"`

	Insecure bool `pkl:"insecure"`

	ServiceName string `pkl:"serviceName"`

	// Deployment environment resource attribute.
	Environment string `pkl:"environment"`

	// Trace sampling ratio (0.0 = none, 1.0 = all).
	TraceSampleRatio float64 `pkl:"traceSampleRatio"`

	MetricIntervalSeconds uint16 `pkl:"metricIntervalSeconds"`
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Otel
func LoadFromPath(ctx context.Context, path string) (ret Otel, err error) {
	evaluator, err := pkl.NewEvaluator(ctx, pkl.PreconfiguredOptions)
	if err != nil {
		return ret, err
	}
	defer func() {
		cerr := evaluator.Close()
		if err == nil {
			err = cerr
		}
	}()
	ret, err = Load(ctx, evaluator, pkl.FileSource(path))
	return ret, err
}

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Otel
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Otel, error) {
	var ret Otel
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
