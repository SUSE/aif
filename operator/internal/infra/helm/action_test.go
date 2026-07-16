/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"testing"
)

func TestReleaseNeedsUpgrade(t *testing.T) {
	tests := []struct {
		name string
		info ReleaseInfo
		spec ReleaseSpec
		want bool
	}{
		{
			name: "same version and no values requires no upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: nil},
			spec: ReleaseSpec{Version: "1.0.0", Values: nil},
			want: false,
		},
		{
			name: "same version with nil vs empty values requires no upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: nil},
			spec: ReleaseSpec{Version: "1.0.0", Values: map[string]interface{}{}},
			want: false,
		},
		{
			name: "same version and same values requires no upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: map[string]interface{}{"key": "val"}},
			spec: ReleaseSpec{Version: "1.0.0", Values: map[string]interface{}{"key": "val"}},
			want: false,
		},
		{
			name: "different version requires upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: nil},
			spec: ReleaseSpec{Version: "1.1.0", Values: nil},
			want: true,
		},
		{
			name: "same version with changed value requires upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: map[string]interface{}{"key": "old"}},
			spec: ReleaseSpec{Version: "1.0.0", Values: map[string]interface{}{"key": "new"}},
			want: true,
		},
		{
			name: "same version with added value requires upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: nil},
			spec: ReleaseSpec{Version: "1.0.0", Values: map[string]interface{}{"key": "val"}},
			want: true,
		},
		{
			name: "same version with nested values unchanged requires no upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: map[string]interface{}{"a": map[string]interface{}{"b": "c"}}},
			spec: ReleaseSpec{Version: "1.0.0", Values: map[string]interface{}{"a": map[string]interface{}{"b": "c"}}},
			want: false,
		},
		{
			name: "same version with nested values changed requires upgrade",
			info: ReleaseInfo{Version: "1.0.0", Values: map[string]interface{}{"a": map[string]interface{}{"b": "c"}}},
			spec: ReleaseSpec{Version: "1.0.0", Values: map[string]interface{}{"a": map[string]interface{}{"b": "d"}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseNeedsUpgrade(&tt.info, tt.spec)
			if got != tt.want {
				t.Errorf("releaseNeedsUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionDrift(t *testing.T) {
	tests := []struct {
		name string
		info *ReleaseInfo
		spec ReleaseSpec
		want bool
	}{
		{
			name: "no installed release is not a drift",
			info: nil,
			spec: ReleaseSpec{Version: "1.0.0"},
			want: false,
		},
		{
			name: "matching version is not a drift",
			info: &ReleaseInfo{Version: "1.0.0"},
			spec: ReleaseSpec{Version: "1.0.0"},
			want: false,
		},
		{
			name: "differing pinned version is a drift",
			info: &ReleaseInfo{Version: "1.0.0"},
			spec: ReleaseSpec{Version: "1.1.0"},
			want: true,
		},
		{
			name: "empty requested version differs from installed (matches releaseNeedsUpgrade)",
			info: &ReleaseInfo{Version: "1.0.0"},
			spec: ReleaseSpec{Version: ""},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionDrift(tt.info, tt.spec); got != tt.want {
				t.Errorf("versionDrift() = %v, want %v", got, tt.want)
			}
		})
	}
}
