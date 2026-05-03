package cloudrun_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/anish749/go-run/cloudrun"
)

// unsetEnv clears named env vars for the test. t.Setenv only sets; this is
// the missing complement when we need to exercise an "unset" code path.
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

// stubMetadataServer points GCE_METADATA_HOST at a server that returns the
// given project ID for the project-id endpoint.
func stubMetadataServer(t *testing.T, projectID string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing Metadata-Flavor header", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/computeMetadata/v1/project/project-id" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Metadata-Flavor", "Google")
		_, _ = w.Write([]byte(projectID))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(srv.URL, "http://"))
}

func TestLoadEnv_Local(t *testing.T) {
	unsetEnv(t, "PORT", "K_SERVICE", "K_REVISION", "K_CONFIGURATION")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "test-project")

	got, err := cloudrun.LoadEnv(context.Background())
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if got.Port != 8080 {
		t.Errorf("Port: got %d, want default 8080", got.Port)
	}
	if got.Project != "test-project" {
		t.Errorf("Project: got %q, want test-project", got.Project)
	}
	if got.Runtime != nil {
		t.Errorf("Runtime: got %+v, want nil (local)", got.Runtime)
	}
	if got.IsCloudRun() {
		t.Error("IsCloudRun: got true, want false")
	}
}

func TestLoadEnv_OnCloudRun(t *testing.T) {
	// Simulate Cloud Run: GOOGLE_CLOUD_PROJECT unset, K_* injected, metadata
	// server reachable.
	unsetEnv(t, "GOOGLE_CLOUD_PROJECT")
	t.Setenv("PORT", "9090")
	t.Setenv("K_SERVICE", "my-service")
	t.Setenv("K_REVISION", "my-service-00001-abc")
	t.Setenv("K_CONFIGURATION", "my-service")
	stubMetadataServer(t, "from-metadata")

	got, err := cloudrun.LoadEnv(context.Background())
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if got.Port != 9090 {
		t.Errorf("Port: got %d, want 9090", got.Port)
	}
	if got.Project != "from-metadata" {
		t.Errorf("Project: got %q, want from-metadata", got.Project)
	}
	if got.Runtime == nil {
		t.Fatal("Runtime: got nil, want populated (on Cloud Run)")
	}
	if got.Runtime.Service != "my-service" {
		t.Errorf("Service: got %q", got.Runtime.Service)
	}
	if got.Runtime.Revision != "my-service-00001-abc" {
		t.Errorf("Revision: got %q", got.Runtime.Revision)
	}
	if got.Runtime.Configuration != "my-service" {
		t.Errorf("Configuration: got %q", got.Runtime.Configuration)
	}
	if !got.IsCloudRun() {
		t.Error("IsCloudRun: got false, want true")
	}
}

func TestLoadEnv_ProjectFromEnvShortCircuitsMetadata(t *testing.T) {
	// When GOOGLE_CLOUD_PROJECT is set, LoadEnv must not hit the metadata server.
	unsetEnv(t, "K_SERVICE", "K_REVISION", "K_CONFIGURATION")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "from-env")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected metadata server hit: %s", r.URL.Path)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(srv.URL, "http://"))

	got, err := cloudrun.LoadEnv(context.Background())
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if got.Project != "from-env" {
		t.Errorf("Project: got %q, want from-env", got.Project)
	}
}

func TestLoadEnv_PartialRuntimeVarsErrors(t *testing.T) {
	// K_SERVICE alone (without K_REVISION, K_CONFIGURATION) is a misconfigured
	// environment — must error rather than produce a half-populated Runtime.
	unsetEnv(t, "K_REVISION", "K_CONFIGURATION")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "test-project")
	t.Setenv("K_SERVICE", "my-service")

	_, err := cloudrun.LoadEnv(context.Background())
	if err == nil {
		t.Fatal("LoadEnv: got nil error; expected error for partial K_* vars")
	}
	if !strings.Contains(err.Error(), "partial Cloud Run runtime variables") {
		t.Errorf("error should describe partial-vars condition; got: %v", err)
	}
}

func TestLoadEnv_InvalidPortErrors(t *testing.T) {
	unsetEnv(t, "K_SERVICE", "K_REVISION", "K_CONFIGURATION")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "test-project")
	t.Setenv("PORT", "not-a-number")

	_, err := cloudrun.LoadEnv(context.Background())
	if err == nil {
		t.Fatal("LoadEnv: got nil error; expected error for invalid PORT")
	}
}
