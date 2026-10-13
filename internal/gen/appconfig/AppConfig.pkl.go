// Code generated from Pkl module `alexandria.AppConfig`. DO NOT EDIT.
package appconfig

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/ai"
	"github.com/bmurray/alexandria/internal/gen/auth"
	"github.com/bmurray/alexandria/internal/gen/otel"
)

// Application configuration schema for Alexandria. Runtime files amend this
// (`amends "@schema/AppConfig.pkl"`). CONFIG_FILE is the only environment
// variable the process reads.
type AppConfig struct {
	LogLevel string `pkl:"logLevel"`

	Server Server `pkl:"server"`

	Database Database `pkl:"database"`

	Library Library `pkl:"library"`

	Auth auth.Auth `pkl:"auth"`

	Ai ai.Ai `pkl:"ai"`

	Otel otel.Otel `pkl:"otel"`
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a AppConfig
func LoadFromPath(ctx context.Context, path string) (ret AppConfig, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a AppConfig
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (AppConfig, error) {
	var ret AppConfig
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
