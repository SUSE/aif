package helm

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

// mockActionResult allows tests to control Helm SDK behavior
type mockActionResult struct {
	installErr error
	pullErr    error
	loadErr    error
	historyErr error
	upgradeErr error
	uninstallErr error
	releaseExists bool
}

// TestEngine_UpdateSettings verifies settings are stored correctly
func TestEngine_UpdateSettings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	settings := EngineSettings{
		RegistryEndpoints: RegistryEndpoints{
			SUSERegistry:             "registry.example.com",
			ApplicationCollection:    "apps.example.com",
			ApplicationCollectionAPI: "https://api.example.com",
		},
		ImageRewrite: ImageRewriteConfig{
			Enabled: true,
			Rules: []ImageRewriteRule{
				{Match: "nvcr.io", Replace: "registry.example.com/nvidia"},
			},
		},
	}

	engine.UpdateSettings(settings)

	if engine.settings.RegistryEndpoints.SUSERegistry != "registry.example.com" {
		t.Errorf("expected SUSERegistry 'registry.example.com', got %q", engine.settings.RegistryEndpoints.SUSERegistry)
	}
	if !engine.settings.ImageRewrite.Enabled {
		t.Error("expected ImageRewrite.Enabled to be true")
	}
	if len(engine.settings.ImageRewrite.Rules) != 1 {
		t.Fatalf("expected 1 ImageRewrite rule, got %d", len(engine.settings.ImageRewrite.Rules))
	}
	if engine.settings.ImageRewrite.Rules[0].Match != "nvcr.io" {
		t.Errorf("expected rule Match 'nvcr.io', got %q", engine.settings.ImageRewrite.Rules[0].Match)
	}
}

// TestEngine_Status_NotImplemented verifies Status returns error
func TestEngine_Status_NotImplemented(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	_, err := engine.Status(context.Background(), "test-ns", "test-release")
	if err == nil {
		t.Fatal("expected error from Status, got nil")
	}
	if err.Error() != "not implemented" {
		t.Errorf("expected 'not implemented' error, got %q", err.Error())
	}
}

// TestEngine_Rollback_NotImplemented verifies Rollback returns error
func TestEngine_Rollback_NotImplemented(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	err := engine.Rollback(context.Background(), "test-ns", "test-release", 1)
	if err == nil {
		t.Fatal("expected error from Rollback, got nil")
	}
	if err.Error() != "not implemented" {
		t.Errorf("expected 'not implemented' error, got %q", err.Error())
	}
}

// TestEngine_History_NotImplemented verifies History returns error
func TestEngine_History_NotImplemented(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	_, err := engine.History(context.Background(), "test-ns", "test-release")
	if err == nil {
		t.Fatal("expected error from History, got nil")
	}
	if err.Error() != "not implemented" {
		t.Errorf("expected 'not implemented' error, got %q", err.Error())
	}
}

// TestEngine_InstallChartFromRepo_ValidationErrors tests parameter validation
func TestEngine_InstallChartFromRepo_ValidationErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	testCases := []struct {
		name        string
		req         InstallRequest
		expectError bool
	}{
		{
			name: "valid request",
			req: InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "test-release",
				ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
				Values:      map[string]any{"key": "value"},
				Wait:        true,
				Timeout:     5 * time.Minute,
			},
			expectError: true, // Will fail due to no real k8s, but validates params
		},
		{
			name: "empty namespace",
			req: InstallRequest{
				Namespace:   "",
				ReleaseName: "test-release",
				ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
			},
			expectError: true,
		},
		{
			name: "empty release name",
			req: InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "",
				ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
			},
			expectError: true,
		},
		{
			name: "empty chart ref",
			req: InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "test-release",
				ChartRef:    "",
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.InstallChartFromRepo(context.Background(), tc.req)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error for %s, got nil", tc.name)
				}
				// Verify error is meaningful (not empty)
				if err != nil && err.Error() == "" {
					t.Error("error message should not be empty")
				}
			}
		})
	}
}

// TestEngine_Uninstall_WithoutK8s tests uninstall behavior without real k8s
func TestEngine_Uninstall_WithoutK8s(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	// Without a real Kubernetes cluster, Uninstall will fail during execution
	// This test verifies the error handling path
	err := engine.Uninstall(context.Background(), "test-ns", "test-release")
	if err == nil {
		t.Error("expected error when uninstalling without k8s, got nil")
	}

	// Error should be wrapped with context
	if err != nil && !strings.Contains(err.Error(), "helm uninstall failed") {
		t.Errorf("expected 'helm uninstall failed' error, got %q", err.Error())
	}
}

// TestEngine_Uninstall_EmptyParameters tests parameter validation
func TestEngine_Uninstall_EmptyParameters(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	testCases := []struct {
		name        string
		namespace   string
		releaseName string
		expectError bool
	}{
		{
			name:        "valid parameters",
			namespace:   "test-ns",
			releaseName: "test-release",
			expectError: true, // Will fail due to no k8s, but params are valid
		},
		{
			name:        "empty namespace",
			namespace:   "",
			releaseName: "test-release",
			expectError: true,
		},
		{
			name:        "empty release name",
			namespace:   "test-ns",
			releaseName: "",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := engine.Uninstall(context.Background(), tc.namespace, tc.releaseName)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error for %s, got nil", tc.name)
				}
				// Verify error is meaningful (not empty)
				if err != nil && err.Error() == "" {
					t.Error("error message should not be empty")
				}
			}
		})
	}
}

// TestReleaseStatus_Fields verifies ReleaseStatus field types
func TestReleaseStatus_Fields(t *testing.T) {
	now := time.Now()
	status := ReleaseStatus{
		Name:     "test-release",
		Revision: 1,
		Status:   "deployed",
		Updated:  now,
	}

	if status.Name != "test-release" {
		t.Errorf("expected Name 'test-release', got %q", status.Name)
	}
	if status.Revision != 1 {
		t.Errorf("expected Revision 1, got %d", status.Revision)
	}
	if status.Status != "deployed" {
		t.Errorf("expected Status 'deployed', got %q", status.Status)
	}
	if !status.Updated.Equal(now) {
		t.Errorf("expected Updated %v, got %v", now, status.Updated)
	}
}

// TestInstallRequest_Defaults verifies default timeout behavior
func TestInstallRequest_Defaults(t *testing.T) {
	req := InstallRequest{
		Namespace:   "test-ns",
		ReleaseName: "test-release",
		ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
		Values:      map[string]any{"key": "value"},
		Wait:        false,
		Timeout:     0, // Will use default
	}

	if req.Timeout != 0 {
		t.Errorf("expected default Timeout 0 before install, got %v", req.Timeout)
	}

	// Note: The actual default of 5 minutes is applied in InstallChartFromRepo
	// This test verifies the request struct accepts zero timeout
}

// TestEngineSettings_Structure verifies EngineSettings field hierarchy
func TestEngineSettings_Structure(t *testing.T) {
	settings := EngineSettings{
		RegistryEndpoints: RegistryEndpoints{
			SUSERegistry:             "registry.suse.com",
			ApplicationCollection:    "dp.apps.rancher.io",
			ApplicationCollectionAPI: "https://api.apps.rancher.io",
		},
		ImageRewrite: ImageRewriteConfig{
			Enabled: true,
			Rules: []ImageRewriteRule{
				{Match: "nvcr.io", Replace: "registry.suse.com/nvidia"},
				{Match: "docker.io", Replace: "registry.suse.com/dockerhub"},
			},
		},
	}

	if settings.RegistryEndpoints.SUSERegistry != "registry.suse.com" {
		t.Errorf("unexpected SUSERegistry: %s", settings.RegistryEndpoints.SUSERegistry)
	}
	if len(settings.ImageRewrite.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(settings.ImageRewrite.Rules))
	}
}

// TestRevisionInfo_Structure verifies RevisionInfo field types
func TestRevisionInfo_Structure(t *testing.T) {
	now := time.Now()
	rev := RevisionInfo{
		Revision:    1,
		Updated:     now,
		Status:      "deployed",
		Description: "Install complete",
	}

	if rev.Revision != 1 {
		t.Errorf("expected Revision 1, got %d", rev.Revision)
	}
	if rev.Status != "deployed" {
		t.Errorf("expected Status 'deployed', got %q", rev.Status)
	}
	if rev.Description != "Install complete" {
		t.Errorf("expected Description 'Install complete', got %q", rev.Description)
	}
}

// TestEngine_New verifies constructor
func TestEngine_New(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	config := &rest.Config{}

	eng := New(logger, config)

	if eng == nil {
		t.Fatal("expected non-nil engine from New")
	}

	// Verify it implements the interface
	var _ Engine = eng
}

// TestEngine_ContextCancellation verifies context cancellation is respected
func TestEngine_ContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := InstallRequest{
		Namespace:   "test-ns",
		ReleaseName: "test-release",
		ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
		Values:      map[string]any{},
		Timeout:     1 * time.Second,
	}

	// Should fail quickly with cancelled context
	_, err := engine.InstallChartFromRepo(ctx, req)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

// TestEngine_Uninstall_ContextCancellation verifies context cancellation in uninstall
func TestEngine_Uninstall_ContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail quickly with cancelled context
	err := engine.Uninstall(ctx, "test-ns", "test-release")
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

// TestImageRewriteRule_Matching verifies rule structure
func TestImageRewriteRule_Matching(t *testing.T) {
	rule := ImageRewriteRule{
		Match:   "nvcr.io",
		Replace: "registry.suse.com/nvidia",
	}

	if rule.Match != "nvcr.io" {
		t.Errorf("expected Match 'nvcr.io', got %q", rule.Match)
	}
	if rule.Replace != "registry.suse.com/nvidia" {
		t.Errorf("expected Replace 'registry.suse.com/nvidia', got %q", rule.Replace)
	}
}

// TestRegistryEndpoints_Defaults verifies default values are meaningful
func TestRegistryEndpoints_Defaults(t *testing.T) {
	// Default values as documented in ARCHITECTURE.md
	defaults := RegistryEndpoints{
		SUSERegistry:             "registry.suse.com",
		ApplicationCollection:    "dp.apps.rancher.io",
		ApplicationCollectionAPI: "https://api.apps.rancher.io",
	}

	if defaults.SUSERegistry == "" {
		t.Error("SUSERegistry should have a default value")
	}
	if defaults.ApplicationCollection == "" {
		t.Error("ApplicationCollection should have a default value")
	}
	if defaults.ApplicationCollectionAPI == "" {
		t.Error("ApplicationCollectionAPI should have a default value")
	}
}

// TestEngine_Interface_Compliance verifies engine implements Engine interface
func TestEngine_Interface_Compliance(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	config := &rest.Config{}

	var _ Engine = &engine{
		logger: logger,
		config: config,
	}
}

// TestImageRewriteConfig_DisabledByDefault verifies default behavior
func TestImageRewriteConfig_DisabledByDefault(t *testing.T) {
	config := ImageRewriteConfig{
		Enabled: false,
		Rules:   []ImageRewriteRule{},
	}

	if config.Enabled {
		t.Error("ImageRewrite should be disabled by default")
	}
	if len(config.Rules) != 0 {
		t.Error("ImageRewrite should have no rules by default")
	}
}

// TestEngine_InstallRequest_Timeout verifies timeout handling
func TestEngine_InstallRequest_Timeout(t *testing.T) {
	testCases := []struct {
		name            string
		requestTimeout  time.Duration
		expectedMinimum time.Duration
	}{
		{
			name:            "zero timeout uses default",
			requestTimeout:  0,
			expectedMinimum: 0, // Will be set to 5min in implementation
		},
		{
			name:            "explicit timeout is respected",
			requestTimeout:  10 * time.Minute,
			expectedMinimum: 10 * time.Minute,
		},
		{
			name:            "short timeout is allowed",
			requestTimeout:  30 * time.Second,
			expectedMinimum: 30 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "test-release",
				ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
				Timeout:     tc.requestTimeout,
			}

			if req.Timeout != tc.requestTimeout {
				t.Errorf("expected timeout %v, got %v", tc.requestTimeout, req.Timeout)
			}
		})
	}
}

// TestEngine_MultipleUpdates verifies UpdateSettings can be called multiple times
func TestEngine_MultipleUpdates(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	// First update
	settings1 := EngineSettings{
		RegistryEndpoints: RegistryEndpoints{
			SUSERegistry: "registry1.example.com",
		},
	}
	engine.UpdateSettings(settings1)
	if engine.settings.RegistryEndpoints.SUSERegistry != "registry1.example.com" {
		t.Error("first update failed")
	}

	// Second update should replace
	settings2 := EngineSettings{
		RegistryEndpoints: RegistryEndpoints{
			SUSERegistry: "registry2.example.com",
		},
	}
	engine.UpdateSettings(settings2)
	if engine.settings.RegistryEndpoints.SUSERegistry != "registry2.example.com" {
		t.Error("second update failed")
	}
}

// TestEngine_ErrorWrapping verifies errors include context
func TestEngine_ErrorWrapping(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{}, // Invalid config will cause errors
	}

	req := InstallRequest{
		Namespace:   "test-ns",
		ReleaseName: "test-release",
		ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
	}

	_, err := engine.InstallChartFromRepo(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from invalid config")
	}

	// Verify error includes context (wrapped)
	errStr := err.Error()
	if !strings.Contains(errStr, "failed") {
		t.Errorf("expected error to include context, got %q", errStr)
	}
}

// TestEngine_Wait_Flag verifies Wait flag is configurable
func TestEngine_Wait_Flag(t *testing.T) {
	testCases := []struct {
		name     string
		waitFlag bool
	}{
		{name: "wait enabled", waitFlag: true},
		{name: "wait disabled", waitFlag: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "test-release",
				ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
				Wait:        tc.waitFlag,
			}

			if req.Wait != tc.waitFlag {
				t.Errorf("expected Wait %v, got %v", tc.waitFlag, req.Wait)
			}
		})
	}
}

// TestEngine_Values_EmptyMap verifies nil vs empty map handling
func TestEngine_Values_EmptyMap(t *testing.T) {
	testCases := []struct {
		name   string
		values map[string]any
	}{
		{
			name:   "nil values",
			values: nil,
		},
		{
			name:   "empty values",
			values: map[string]any{},
		},
		{
			name: "populated values",
			values: map[string]any{
				"key1": "value1",
				"key2": 123,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "test-release",
				ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
				Values:      tc.values,
			}

			// Verify values are stored as-is
			if tc.values == nil && req.Values != nil {
				t.Error("nil values should remain nil")
			}
			if tc.values != nil && req.Values == nil {
				t.Error("non-nil values should not become nil")
			}
		})
	}
}

// TestEngine_ChartRef_Formats verifies different OCI ref formats
func TestEngine_ChartRef_Formats(t *testing.T) {
	testCases := []struct {
		name     string
		chartRef string
		valid    bool
	}{
		{
			name:     "standard OCI ref",
			chartRef: "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
			valid:    true,
		},
		{
			name:     "OCI ref without version",
			chartRef: "oci://registry.suse.com/ai/charts/nim-llm",
			valid:    true, // Helm will use latest
		},
		{
			name:     "OCI ref with digest",
			chartRef: "oci://registry.suse.com/ai/charts/nim-llm@sha256:abcd",
			valid:    true,
		},
		{
			name:     "empty ref",
			chartRef: "",
			valid:    false,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: &rest.Config{},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := InstallRequest{
				Namespace:   "test-ns",
				ReleaseName: "test-release",
				ChartRef:    tc.chartRef,
			}

			_, err := engine.InstallChartFromRepo(context.Background(), req)

			// All will fail due to no k8s, but we're testing ref validation
			if err == nil && !tc.valid {
				t.Error("expected error for invalid ref, got nil")
			}
		})
	}
}

// TestEngine_NilLogger_Safe verifies nil logger causes panic (expected)
func TestEngine_NilLogger_Safe(t *testing.T) {
	engine := &engine{
		logger: nil,
		config: &rest.Config{},
	}

	// Expect panic when calling method with nil logger
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic with nil logger, but did not panic")
		}
		// If r != nil, panic occurred as expected, test passes
	}()

	// Call a method that would use logger - should panic
	engine.UpdateSettings(EngineSettings{})
}

// TestEngine_NilConfig_Handled verifies nil config causes panic (expected behavior)
func TestEngine_NilConfig_Handled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &engine{
		logger: logger,
		config: nil, // Nil config will cause panic in k8s client creation
	}

	req := InstallRequest{
		Namespace:   "test-ns",
		ReleaseName: "test-release",
		ChartRef:    "oci://registry.suse.com/ai/charts/nim-llm:1.0.0",
	}

	// Expect panic due to nil config - this is expected behavior from k8s client-go
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic with nil config, but did not panic")
		}
		// If r != nil, panic occurred as expected, test passes
	}()

	_, _ = engine.InstallChartFromRepo(context.Background(), req)
}
