// Package cloudrun provides adapters for code running on Google Cloud Run.
//
// Env captures the runtime environment variables Cloud Run injects into a
// service container, plus the GCP project ID that GCP client libraries
// typically need. It is intended to be embedded in your own config struct:
//
//	type MyEnv struct {
//	    cloudrun.Env
//	    DBURL string `env:"DB_URL,notEmpty"`
//	}
//	var cfg MyEnv
//	if err := env.Parse(&cfg); err != nil { ... }
//	if err := cloudrun.ResolveProjectID(ctx, &cfg.Env); err != nil { ... }
//
// The split between env.Parse and ResolveProjectID exists because Cloud Run
// does not inject GOOGLE_CLOUD_PROJECT — it must come from either the user
// (locally) or the GCE metadata server (on Cloud Run).
package cloudrun

import (
	"context"
	"fmt"

	"cloud.google.com/go/compute/metadata"
)

// Env holds the runtime environment of a service running on Cloud Run.
//
// PORT, K_SERVICE, K_REVISION, and K_CONFIGURATION are injected automatically
// by Cloud Run. They are empty when the process runs locally (PORT defaults
// to 8080 here so a local run still has a sensible value).
//
// GOOGLE_CLOUD_PROJECT is NOT injected by Cloud Run. Locally, set it in your
// shell or .env file. On Cloud Run, leave it unset and call ResolveProjectID
// to look it up from the metadata server.
type Env struct {
	// Port is the HTTP listen port. Cloud Run injects PORT=8080 by default.
	Port int `env:"PORT" envDefault:"8080"`

	// Project is the GCP project ID, sourced from GOOGLE_CLOUD_PROJECT.
	// Empty if neither set in the environment nor resolved via ResolveProjectID.
	Project string `env:"GOOGLE_CLOUD_PROJECT"`

	// Service is the Cloud Run service name (K_SERVICE). Empty when local.
	Service string `env:"K_SERVICE"`

	// Revision is the Cloud Run revision name (K_REVISION). Empty when local.
	Revision string `env:"K_REVISION"`

	// Configuration is the Cloud Run Configuration name (K_CONFIGURATION).
	// Empty when local.
	Configuration string `env:"K_CONFIGURATION"`
}

// IsRunningOnCloudRun reports whether the process is running on Cloud Run.
// It infers this from the presence of K_SERVICE, which Cloud Run always sets.
func (e Env) IsRunningOnCloudRun() bool {
	return e.Service != ""
}

// ResolveProjectID populates e.Project if it is empty, by querying the GCE
// metadata server. The metadata server is reachable from Cloud Run, GCE, GKE,
// and a few other GCP runtimes; locally it is not, so users must set
// GOOGLE_CLOUD_PROJECT in their environment.
//
// If e.Project is already set this is a no-op. If the metadata server is not
// reachable and the env var was not set, it returns an error.
//
// The metadata server endpoint can be overridden for tests via the
// GCE_METADATA_HOST environment variable.
func ResolveProjectID(ctx context.Context, e *Env) error {
	if e.Project != "" {
		return nil
	}
	proj, err := metadata.ProjectIDWithContext(ctx)
	if err != nil {
		return fmt.Errorf("cloudrun: GOOGLE_CLOUD_PROJECT not set and metadata server unreachable: %w", err)
	}
	e.Project = proj
	return nil
}
