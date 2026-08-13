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
	"os"
	"path/filepath"
	"testing"

	"helm.sh/helm/v3/pkg/chartutil"
)

// The archive handed back has to be the one the chart was parsed from, because
// that is the whole basis on which the cache may keep it: the file it was read
// out of is named for the chart and version alone, so it is not a name for these
// bytes and can be a different chart by the next read.
func TestLoadLocalChartReturnsTheArchiveItParsed(t *testing.T) {
	dir := t.TempDir()
	path, err := chartutil.Save(testChart("2.1.0"), dir)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	ch, archive, err := loadLocalChart(path)
	if err != nil {
		t.Fatalf("loadLocalChart() error = %v", err)
	}
	if ch.Metadata.Version != "2.1.0" {
		t.Errorf("chart version = %q, want 2.1.0", ch.Metadata.Version)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the artifact: %v", err)
	}
	if string(archive) != string(onDisk) {
		t.Error("the archive returned is not the file that was parsed")
	}

	// And it round-trips: a hit re-parsing it must get the same chart back.
	again, err := loadArchive(archive)
	if err != nil {
		t.Fatalf("loadArchive() error = %v", err)
	}
	if again.Metadata.Version != ch.Metadata.Version {
		t.Errorf("re-parsed version = %q, want %q", again.Metadata.Version, ch.Metadata.Version)
	}
}

// LocateChart resolves an unpacked chart directory as well as an archive. No spec
// this operator builds reaches that, but the loader still has to, and there are
// no bytes to hand the cache when it does — a directory is not a download, and
// caching the path to one would put the cache back on a name it does not own.
func TestLoadLocalChartLoadsADirectoryAndCachesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := chartutil.SaveDir(testChart("2.1.0"), dir); err != nil {
		t.Fatalf("SaveDir() error = %v", err)
	}

	ch, archive, err := loadLocalChart(filepath.Join(dir, testRelName))
	if err != nil {
		t.Fatalf("loadLocalChart() error = %v", err)
	}
	if ch.Metadata.Version != "2.1.0" {
		t.Errorf("chart version = %q, want 2.1.0", ch.Metadata.Version)
	}
	if archive != nil {
		t.Errorf("got %d bytes of archive for an unpacked chart, want none", len(archive))
	}
}

// A chart that does not parse is an error, not an empty chart handed on to a
// render that would then diff against nothing.
func TestLoadLocalChartRejectsWhatIsNotAChart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aif-ui-2.1.0.tgz")
	if err := os.WriteFile(path, []byte("not a gzip stream"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	if _, _, err := loadLocalChart(path); err == nil {
		t.Error("loadLocalChart() accepted a file that is not a chart archive")
	}
}
