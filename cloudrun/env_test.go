package cloudrun_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/anish749/go-run/cloudrun"
	"github.com/caarlos0/env/v11"
)

// unsetEnv clears the named env vars for the test and restores them after.
// t.Setenv only sets; this is its missing complement when we need a var to
// look genuinely unset (e.g. to exercise a default or a metadata fallback).
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	saved := map[string]string{}
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
		}
		os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	})
}

func TestEnv_LocalDefaults(t *testing.T) {
	unsetEnv(t, "PORT", "K_SERVICE", "K_REVISION", "K_CONFIGURATION", "GOOGLE_CLOUD_PROJECT")

	var got cloudrun.Env
	if err := env.Parse(&got); err != nil {
		t.Fatalf("env.Parse: %v", err)
	}
	if got.Port != 8080 {
		t.Errorf("Port: got %d, want default 8080", got.Port)
	}
	if got.Service != "" {
		t.Errorf("Service: got %q, want empty (local)", got.Service)
	}
	if got.IsRunningOnCloudRun() {
		t.Error("IsRunningOnCloudRun: got true, want false (no K_SERVICE)")
	}
}

func TestEnv_OnCloudRun(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("K_SERVICE", "my-service")
	t.Setenv("K_REVISION", "my-service-00001-abc")
	t.Setenv("K_CONFIGURATION", "my-service")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")

	var got cloudrun.Env
	if err := env.Parse(&got); err != nil {
		t.Fatalf("env.Parse: %v", err)
	}
	if got.Port != 9090 {
		t.Errorf("Port: got %d, want 9090", got.Port)
	}
	if got.Service != "my-service" {
		t.Errorf("Service: got %q, want my-service", got.Service)
	}
	if got.Revision != "my-service-00001-abc" {
		t.Errorf("Revision: got %q", got.Revision)
	}
	if got.Configuration != "my-service" {
		t.Errorf("Configuration: got %q", got.Configuration)
	}
	if got.Project != "my-project" {
		t.Errorf("Project: got %q, want my-project", got.Project)
	}
	if !got.IsRunningOnCloudRun() {
		t.Error("IsRunningOnCloudRun: got false, want true")
	}
}

func TestEnv_Embedding(t *testing.T) {
	unsetEnv(t, "K_SERVICE", "K_REVISION", "K_CONFIGURATION")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "test-project")
	t.Setenv("DB_URL", "postgres://localhost/x")

	type MyEnv struct {
		cloudrun.Env
		DBURL string `env:"DB_URL,notEmpty"`
	}

	var got MyEnv
	if err := env.Parse(&got); err != nil {
		t.Fatalf("env.Parse: %v", err)
	}
	if got.Project != "test-project" {
		t.Errorf("Project: got %q, want test-project", got.Project)
	}
	if got.DBURL != "postgres://localhost/x" {
		t.Errorf("DBURL: got %q", got.DBURL)
	}
}

func TestResolveProjectID_PrefersEnvVar(t *testing.T) {
	// If the env var is set, we should not hit the metadata server.
	// We point GCE_METADATA_HOST at a server that would fail the test
	// if it were ever called.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected metadata server hit: %s", r.URL.Path)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(srv.URL, "http://"))

	e := &cloudrun.Env{Project: "from-env"}
	if err := cloudrun.ResolveProjectID(context.Background(), e); err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	if e.Project != "from-env" {
		t.Errorf("Project: got %q, want from-env", e.Project)
	}
}

func TestResolveProjectID_FallsBackToMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The GCP metadata client requires this header on every request.
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing Metadata-Flavor header", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/computeMetadata/v1/project/project-id":
			w.Header().Set("Metadata-Flavor", "Google")
			w.Write([]byte("from-metadata"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(srv.URL, "http://"))

	e := &cloudrun.Env{}
	if err := cloudrun.ResolveProjectID(context.Background(), e); err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	if e.Project != "from-metadata" {
		t.Errorf("Project: got %q, want from-metadata", e.Project)
	}
}

// Note: there is intentionally no test for the "metadata unreachable" path.
// cloud.google.com/go/compute/metadata caches successful project-ID lookups
// in a package-global, making such a test order-dependent across the suite.
// The error path in ResolveProjectID is a one-line wrap; verified by reading.
