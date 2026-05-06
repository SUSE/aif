package manager

import (
	"testing"

	aifv1alpha1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
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
	scheme := runtime.NewScheme()
	require.NoError(t, aifv1alpha1.AddToScheme(scheme))

	cfg := &rest.Config{Host: "http://localhost:1"}

	mgr, err := NewManager(scheme, cfg, Options{})
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}
