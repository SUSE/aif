package helm

import (
	"errors"
	"testing"
)

func TestFindChartAnnotations(t *testing.T) {
	indexYAML := []byte(`
entries:
  my-extension:
  - name: my-extension
    version: "1.2.0"
    annotations:
      catalog.cattle.io/display-name: My Extension
      catalog.cattle.io/rancher-version: ">= 2.10.0"
      catalog.cattle.io/ui-extensions-version: ">= 3.0.0 < 4.0.0"
  - name: my-extension
    version: "1.1.0"
    annotations:
      catalog.cattle.io/display-name: My Extension Old
`)

	tests := []struct {
		name         string
		chartName    string
		chartVersion string
		wantDisplay  string
		wantErr      error
	}{
		{
			name:         "happy path",
			chartName:    "my-extension",
			chartVersion: "1.2.0",
			wantDisplay:  "My Extension",
		},
		{
			name:         "older version",
			chartName:    "my-extension",
			chartVersion: "1.1.0",
			wantDisplay:  "My Extension Old",
		},
		{
			name:         "chart not found",
			chartName:    "nonexistent",
			chartVersion: "1.0.0",
			wantErr:      ErrChartNotFound,
		},
		{
			name:         "version not found",
			chartName:    "my-extension",
			chartVersion: "9.9.9",
			wantErr:      ErrVersionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ann, err := FindChartAnnotations(indexYAML, tt.chartName, tt.chartVersion)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error wrapping %v, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ann.DisplayName != tt.wantDisplay {
				t.Errorf("DisplayName = %q, want %q", ann.DisplayName, tt.wantDisplay)
			}
		})
	}
}

func TestFindChartAnnotations_NoAnnotations(t *testing.T) {
	indexYAML := []byte(`
entries:
  bare-chart:
  - name: bare-chart
    version: "1.0.0"
`)

	ann, err := FindChartAnnotations(indexYAML, "bare-chart", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ann.DisplayName != "" {
		t.Errorf("expected empty DisplayName, got %q", ann.DisplayName)
	}
	if ann.RancherVersion != "" {
		t.Errorf("expected empty RancherVersion, got %q", ann.RancherVersion)
	}
	if ann.ExtensionsVersion != "" {
		t.Errorf("expected empty ExtensionsVersion, got %q", ann.ExtensionsVersion)
	}
}

func TestFindChartAnnotations_InvalidYAML(t *testing.T) {
	_, err := FindChartAnnotations([]byte(`{{{not yaml`), "x", "1.0.0")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
