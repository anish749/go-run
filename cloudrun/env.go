// Package cloudrun provides adapters for code running on Google Cloud Run.
//
// LoadEnv is the single constructor for runtime configuration:
//
//	cloud, err := cloudrun.LoadEnv(ctx)
//	if err != nil { /* ... */ }
//
// The returned Env is always fully populated: Project is resolved (from
// GOOGLE_CLOUD_PROJECT or the GCE metadata server) and Runtime is either
// nil (running locally) or fully populated (running on Cloud Run). There is
// no partially-built middle state to worry about.
//
// Service-specific configuration (DB URLs, feature flags, etc.) is parsed
// independently — typically with caarlos0/env or your config tool of choice
// — and composed into your service's own Config struct alongside cloudrun.Env.
package cloudrun

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"cloud.google.com/go/compute/metadata"
)

// Runtime is the Cloud Run-injected runtime info. A non-nil *Runtime
// guarantees every field is populated; nil means the process is running
// locally. The presence of K_SERVICE is the disjunction tag — Cloud Run
// always injects all three K_* variables together.
type Runtime struct {
	Service       string // K_SERVICE
	Revision      string // K_REVISION
	Configuration string // K_CONFIGURATION
}

// Env is the resolved runtime environment of a service.
//
// Project is always populated when LoadEnv returns no error.
// Runtime is nil iff running locally; non-nil iff running on Cloud Run.
//
// A zero Env value is never useful — only construct via LoadEnv.
type Env struct {
	Port    int
	Project string
	Runtime *Runtime
}

// IsCloudRun reports whether the process is running on Cloud Run.
// Equivalent to e.Runtime != nil; provided for readability at call sites.
func (e Env) IsCloudRun() bool {
	return e.Runtime != nil
}

// LoadEnv reads the Cloud Run runtime environment and resolves the GCP
// project ID. It returns a fully-populated Env or an error — never a
// half-built value.
//
// Project resolution order:
//  1. GOOGLE_CLOUD_PROJECT environment variable, if set.
//  2. The GCE metadata server (available on Cloud Run, GCE, GKE, etc.).
//
// Locally, set GOOGLE_CLOUD_PROJECT in your shell or .env. On Cloud Run,
// leave it unset and the metadata server provides the value automatically.
//
// PORT defaults to 8080 if unset, matching Cloud Run's default. K_SERVICE,
// K_REVISION, and K_CONFIGURATION are read together; if any one is set,
// all three must be (Cloud Run always injects them together — a partial
// set indicates a misconfigured environment and returns an error).
func LoadEnv(ctx context.Context) (Env, error) {
	port, err := readPort()
	if err != nil {
		return Env{}, err
	}
	runtime, err := readRuntime()
	if err != nil {
		return Env{}, err
	}
	project, err := resolveProject(ctx)
	if err != nil {
		return Env{}, err
	}
	return Env{
		Port:    port,
		Project: project,
		Runtime: runtime,
	}, nil
}

func readPort() (int, error) {
	raw := os.Getenv("PORT")
	if raw == "" {
		return 8080, nil
	}
	p, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("cloudrun: PORT=%q is not a valid integer: %w", raw, err)
	}
	return p, nil
}

func readRuntime() (*Runtime, error) {
	service := os.Getenv("K_SERVICE")
	revision := os.Getenv("K_REVISION")
	configuration := os.Getenv("K_CONFIGURATION")

	anySet := service != "" || revision != "" || configuration != ""
	allSet := service != "" && revision != "" && configuration != ""
	if !anySet {
		return nil, nil
	}
	if !allSet {
		return nil, fmt.Errorf(
			"cloudrun: partial Cloud Run runtime variables: K_SERVICE=%q K_REVISION=%q K_CONFIGURATION=%q (Cloud Run injects all three together)",
			service, revision, configuration,
		)
	}
	return &Runtime{
		Service:       service,
		Revision:      revision,
		Configuration: configuration,
	}, nil
}

func resolveProject(ctx context.Context) (string, error) {
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		return v, nil
	}
	proj, err := metadata.ProjectIDWithContext(ctx)
	if err != nil {
		return "", fmt.Errorf("cloudrun: GOOGLE_CLOUD_PROJECT not set and metadata server unreachable: %w", err)
	}
	return proj, nil
}
