package manager

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	aifv1alpha1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/blueprint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestNewManager_NilConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aifv1alpha1.AddToScheme(scheme))

	mgr, err := NewManager(scheme, nil, Options{})
	assert.Nil(t, mgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rest.Config must not be nil")
}

func TestNewManager_NilScheme(t *testing.T) {
	cfg := &rest.Config{Host: "http://localhost:1"}

	mgr, err := NewManager(nil, cfg, Options{})
	assert.Nil(t, mgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must not be nil")
}

func TestOptions_LeaderElectionID_Default(t *testing.T) {
	opts := Options{}
	assert.Equal(t, "aif-operator-leader", opts.leaderElectionID())
}

func TestOptions_LeaderElectionID_Custom(t *testing.T) {
	opts := Options{LeaderElectionID: "custom-id"}
	assert.Equal(t, "custom-id", opts.leaderElectionID())
}

func TestOptions_WebhookPort_Default(t *testing.T) {
	opts := Options{}
	assert.Equal(t, 9443, opts.webhookPort())
}

func TestOptions_WebhookPort_Custom(t *testing.T) {
	opts := Options{WebhookPort: 8443}
	assert.Equal(t, 8443, opts.webhookPort())
}

func TestOptions_WebhookPort_Zero(t *testing.T) {
	opts := Options{WebhookPort: 0}
	assert.Equal(t, 9443, opts.webhookPort())
}

func TestNewManager_Success(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") != "" {
		t.Skip("skipping: TestNewManager_StartWithEnvtest covers this with a real kube-apiserver")
	}

	scheme := runtime.NewScheme()
	require.NoError(t, aifv1alpha1.AddToScheme(scheme))

	cfg := &rest.Config{Host: "http://localhost:1"}

	mgr, err := NewManager(scheme, cfg, Options{})
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestNewManager_StartWithEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest test (run 'make test-controllers')")
	}

	testEnv := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Paths:              []string{filepath.Join("..", "..", "charts", "aif-operator", "crds")},
			ErrorIfPathMissing: true,
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	defer func() { require.NoError(t, testEnv.Stop()) }()

	require.NoError(t, aifv1alpha1.AddToScheme(scheme.Scheme))

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mgr, err := NewManager(scheme.Scheme, cfg, Options{
		MetricsAddr:      "0",
		HealthAddr:       "0",
		BlueprintManager: blueprint.New(nil),
		Logger:           logger,
	})
	require.NoError(t, err)
	require.NotNil(t, mgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		assert.NoError(t, mgr.Start(ctx))
	}()

	assert.Eventually(t, func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}, 30*time.Second, 250*time.Millisecond)
}
