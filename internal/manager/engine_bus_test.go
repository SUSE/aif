package manager

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/SUSE/aif/internal/controller"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/git"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/source_collection"
)

// fakeDiscovery is a hand-rolled nvidia.Discovery that records UpdateSettings.
// Other Discovery methods are unused by these bus tests and return zero values.
type fakeDiscovery struct {
	mu       sync.Mutex
	Settings nvidia.EngineSettings
	Calls    int
}

func (f *fakeDiscovery) UpdateSettings(s nvidia.EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
	f.Calls++
}
func (f *fakeDiscovery) Index(_ context.Context) ([]nvidia.NIMEntry, error) { return nil, nil }
func (f *fakeDiscovery) Get(_ context.Context, _ string) (nvidia.NIMEntry, error) {
	return nvidia.NIMEntry{}, nil
}
func (f *fakeDiscovery) Refresh(_ context.Context) error { return nil }

// fakeDeployer is a hand-rolled nvidia.Deployer that records UpdateSettings.
type fakeDeployer struct {
	mu       sync.Mutex
	Settings nvidia.EngineSettings
	Calls    int
}

func (f *fakeDeployer) UpdateSettings(s nvidia.EngineSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
	f.Calls++
}
func (f *fakeDeployer) GenerateValues(_ context.Context, _ nvidia.GenerateRequest) (map[string]any, error) {
	return nil, nil
}

// helper: build an engineBus with all engines as recording fakes.
func newTestBus() (controller.SettingsApplier, *helm.FakeEngine, *fleet.FakeBundleEngine, *fleet.FakeGitRepoEngine, *fakeDiscovery, *fakeDeployer, *source_collection.FakeClient) {
	helmFake := helm.NewFake()
	fleetFake := fleet.NewFakeBundleEngine()
	gitRepoFake := &fleet.FakeGitRepoEngine{}
	discFake := &fakeDiscovery{}
	deplFake := &fakeDeployer{}
	appCoFake := &source_collection.FakeClient{}
	bus := NewEngineBus(helmFake, fleetFake, gitRepoFake, discFake, deplFake, appCoFake, testLogger())
	return bus, helmFake, fleetFake, gitRepoFake, discFake, deplFake, appCoFake
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestEngineBus_Apply_PushesToHelm: snapshot with rules → helm fake captured.
func TestEngineBus_Apply_PushesToHelm(t *testing.T) {
	bus, h, _, _, _, _, _ := newTestBus()
	snap := controller.SettingsSnapshot{
		SUSERegistry:          "harbor.example.com",
		AppCollectionRegistry: "dp.apps.rancher.io",
		AppCollectionAPI:      "https://api.apps.rancher.io",
		ImageRewriteEnabled:   true,
		ImageRewriteRules: []controller.ImageRewriteRule{
			{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"},
		},
	}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if h.Settings.RegistryEndpoints.SUSERegistry != "harbor.example.com" {
		t.Errorf("SUSERegistry: got %q", h.Settings.RegistryEndpoints.SUSERegistry)
	}
	if !h.Settings.ImageRewrite.Enabled {
		t.Error("ImageRewrite.Enabled must be true")
	}
	if len(h.Settings.ImageRewrite.Rules) != 1 || h.Settings.ImageRewrite.Rules[0].Match != "registry.suse.com/" {
		t.Errorf("rules: got %#v", h.Settings.ImageRewrite.Rules)
	}
}

// TestEngineBus_Apply_PushesToDiscovery: snapshot → discovery fake captured.
func TestEngineBus_Apply_PushesToDiscovery(t *testing.T) {
	bus, _, _, _, d, _, _ := newTestBus()
	snap := controller.SettingsSnapshot{
		SUSERegistry:      "harbor.example.com",
		SUSERegistryUser:  "u",
		SUSERegistryToken: "t",
	}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if d.Settings.RegistryEndpoint != "harbor.example.com" {
		t.Errorf("RegistryEndpoint: got %q", d.Settings.RegistryEndpoint)
	}
	if d.Settings.Username != "u" || d.Settings.Token != "t" {
		t.Errorf("creds: got user=%q token=%q", d.Settings.Username, d.Settings.Token)
	}
}

// TestEngineBus_Apply_PushesToDeployer: snapshot → deployer fake captured;
// Deployer doesn't need creds (only image hostname).
func TestEngineBus_Apply_PushesToDeployer(t *testing.T) {
	bus, _, _, _, _, dep, _ := newTestBus()
	snap := controller.SettingsSnapshot{SUSERegistry: "harbor.example.com"}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if dep.Settings.RegistryEndpoint != "harbor.example.com" {
		t.Errorf("RegistryEndpoint: got %q", dep.Settings.RegistryEndpoint)
	}
}

// TestEngineBus_Apply_AppCoModeDisabled_PushesEmptyAPIURL: mode=disabled →
// bus passes APIURL="" regardless of AppCollectionAPI value.
func TestEngineBus_Apply_AppCoModeDisabled_PushesEmptyAPIURL(t *testing.T) {
	bus, _, _, _, _, _, ac := newTestBus()
	snap := controller.SettingsSnapshot{
		AppCollectionAPI:  "https://configured.example.com",
		AppCollectionMode: "disabled",
	}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ac.Settings.APIURL != "" {
		t.Errorf("mode=disabled must yield APIURL='', got %q", ac.Settings.APIURL)
	}
}

// TestEngineBus_Apply_AppCoModeAPI_PushesConfiguredURL: mode=api → URL passes through.
func TestEngineBus_Apply_AppCoModeAPI_PushesConfiguredURL(t *testing.T) {
	bus, _, _, _, _, _, ac := newTestBus()
	snap := controller.SettingsSnapshot{
		AppCollectionAPI:  "https://api.example.com",
		AppCollectionMode: "api",
	}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ac.Settings.APIURL != "https://api.example.com" {
		t.Errorf("APIURL: got %q", ac.Settings.APIURL)
	}
}

// TestEngineBus_Apply_AppCoModeRegistryFallback_TreatedAsAPI: registry-fallback
// → URL passes through (current punt; follow-up note 1).
func TestEngineBus_Apply_AppCoModeRegistryFallback_TreatedAsAPI(t *testing.T) {
	bus, _, _, _, _, _, ac := newTestBus()
	snap := controller.SettingsSnapshot{
		AppCollectionAPI:  "https://api.example.com",
		AppCollectionMode: "registry-fallback",
	}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ac.Settings.APIURL != "https://api.example.com" {
		t.Errorf("registry-fallback must pass URL through (punt); got %q", ac.Settings.APIURL)
	}
}

// TestEngineBus_Apply_NeverErrorsToday: locks the no-engine-fails baseline.
// If an engine grows fallibility, this test breaks and forces a deliberate
// update to the bus + reconciler error handling.
func TestEngineBus_Apply_NeverErrorsToday(t *testing.T) {
	bus, _, _, _, _, _, _ := newTestBus()
	if err := bus.Apply(context.Background(), controller.SettingsSnapshot{}); err != nil {
		t.Fatalf("Apply must return nil today: %v", err)
	}
}

// TestEngineBus_Apply_PushesOCIHostToAppCo: regression test against the bug
// where the bus dropped OCIHost from the AppCo projection, silently
// degrading source_collection.AnnotationReader (which checks OCIHost in
// effectiveAnnotationSettings and returns ErrNotConfigured when empty).
func TestEngineBus_Apply_PushesOCIHostToAppCo(t *testing.T) {
	bus, _, _, _, _, _, ac := newTestBus()
	snap := controller.SettingsSnapshot{
		AppCollectionRegistry: "dp.apps.rancher.io",
		AppCollectionAPI:      "https://api.apps.rancher.io",
		AppCollectionMode:     "api",
	}
	if err := bus.Apply(context.Background(), snap); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ac.Settings.OCIHost != "dp.apps.rancher.io" {
		t.Errorf("OCIHost: got %q, want dp.apps.rancher.io (without it, AnnotationReader returns ErrNotConfigured)", ac.Settings.OCIHost)
	}
}

func TestEngineBus_Apply_PropagatesFleetSettings_AllAuthTypes(t *testing.T) {
	tests := []struct {
		name string
		snap controller.SettingsSnapshot
		want fleet.FleetSettings
	}{
		{
			name: "token auth: snapshot.FleetGitAuth.Token → git.GitAuth.Token",
			snap: controller.SettingsSnapshot{
				FleetRepoURL:  "https://git.example.com/fleet.git",
				FleetBranch:   "release",
				FleetAuthType: "token",
				FleetGitAuth:  controller.FleetGitAuth{Token: &controller.FleetGitAuthToken{Token: "ghp_xyz"}},
			},
			want: fleet.FleetSettings{
				GitRepoURL: "https://git.example.com/fleet.git",
				GitBranch:  "release",
				GitAuth:    git.GitAuth{Token: &git.TokenAuth{Token: "ghp_xyz"}},
			},
		},
		{
			name: "ssh auth: snapshot.FleetGitAuth.SSH.PrivateKeyPEM → git.GitAuth.SSH.PrivateKeyPEM",
			snap: controller.SettingsSnapshot{
				FleetRepoURL:  "git@github.com:org/repo.git",
				FleetBranch:   "main",
				FleetAuthType: "ssh",
				FleetGitAuth:  controller.FleetGitAuth{SSH: &controller.FleetGitAuthSSH{PrivateKeyPEM: []byte("PEM_BYTES")}},
			},
			want: fleet.FleetSettings{
				GitRepoURL: "git@github.com:org/repo.git",
				GitBranch:  "main",
				GitAuth:    git.GitAuth{SSH: &git.SSHAuth{PrivateKeyPEM: []byte("PEM_BYTES")}},
			},
		},
		{
			name: "basic auth: snapshot.FleetGitAuth.Basic → git.GitAuth.Basic",
			snap: controller.SettingsSnapshot{
				FleetRepoURL:  "https://git.example.com/fleet.git",
				FleetBranch:   "main",
				FleetAuthType: "basic",
				FleetGitAuth:  controller.FleetGitAuth{Basic: &controller.FleetGitAuthBasic{Username: "", Password: "s3cret"}},
			},
			want: fleet.FleetSettings{
				GitRepoURL: "https://git.example.com/fleet.git",
				GitBranch:  "main",
				GitAuth:    git.GitAuth{Basic: &git.BasicAuth{Username: "", Password: "s3cret"}},
			},
		},
		{
			name: "anonymous: empty FleetGitAuth → zero git.GitAuth",
			snap: controller.SettingsSnapshot{
				FleetRepoURL: "file:///tmp/local.git",
				FleetBranch:  "main",
			},
			want: fleet.FleetSettings{
				GitRepoURL: "file:///tmp/local.git",
				GitBranch:  "main",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bus, _, fb, fg, _, _, _ := newTestBus()
			if err := bus.Apply(context.Background(), tc.snap); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			gotFB := fb.LastSettings()
			gotFG := fg.LastSettings()
			if !reflect.DeepEqual(gotFB, tc.want) {
				t.Errorf("fleetBundle settings mismatch\n got: %+v\nwant: %+v", gotFB, tc.want)
			}
			if !reflect.DeepEqual(gotFG, tc.want) {
				t.Errorf("fleetGitRepo settings mismatch\n got: %+v\nwant: %+v", gotFG, tc.want)
			}
		})
	}
}
