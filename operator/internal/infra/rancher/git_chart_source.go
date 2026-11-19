package rancher

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

// Rancher accepts both indexed archives and repositories of chart directories.
// Inspect Git trees without a checkout: submodules and symlinks are never read.
func gitChartPaths(tree *object.Tree) (indexes, metadata []string, err error) {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for count := 0; ; count++ {
		name, entry, err := walker.Next()
		if err == io.EOF {
			return indexes, metadata, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if count >= 100000 {
			return nil, nil, fmt.Errorf("Git chart repository exceeds 100000 entries; use a smaller mirror")
		}
		if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable {
			continue
		}
		switch path.Base(name) {
		case "index.yaml":
			indexes = append(indexes, name)
		case "Chart.yaml":
			metadata = append(metadata, name)
		}
	}
}

func gitChartIndex(tree *object.Tree) (*repo.IndexFile, []string, error) {
	indexes, metadata, err := gitChartPaths(tree)
	if err != nil {
		return nil, nil, err
	}
	// Match Rancher's choice of the shortest valid index path. Its URLs are
	// relative to the repository root, including when the index is nested.
	sort.SliceStable(indexes, func(i, j int) bool { return len(indexes[i]) < len(indexes[j]) })
	for _, name := range indexes {
		data, err := gitTreeFile(tree, name, maxGitIndexBytes)
		if err != nil {
			return nil, nil, err
		}
		var index repo.IndexFile
		if err := yaml.Unmarshal(data, &index); err == nil && index.APIVersion == "v1" {
			return &index, nil, nil
		}
	}
	return nil, metadata, nil
}

func gitDirectoryChart(tree *object.Tree, metadata []string, name, version string) ([]byte, error) {
	// Do not expose a bundled dependency as a separate repository chart.
	sort.SliceStable(metadata, func(i, j int) bool { return strings.Count(metadata[i], "/") < strings.Count(metadata[j], "/") })
	var chartRoots []string
	for _, filename := range metadata {
		dir := path.Dir(filename)
		nested := false
		for _, root := range chartRoots {
			if root == "." || strings.HasPrefix(dir, root+"/") {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		data, err := gitTreeFile(tree, filename, 1<<20)
		if err != nil {
			return nil, err
		}
		var chart struct{ Name, Version string }
		if err := yaml.Unmarshal(data, &chart); err != nil || chart.Name == "" || chart.Version == "" {
			// Unrelated fixtures can also be named Chart.yaml. Only a chart
			// with usable metadata can match or hide its bundled dependencies.
			continue
		}
		chartRoots = append(chartRoots, dir)
		if chart.Name == name && chart.Version == version {
			return packageGitChart(tree, dir, name, version)
		}
	}
	return nil, fmt.Errorf("chart %s@%s is absent from the indexed Git commit", name, version)
}

func packageGitChart(root *object.Tree, directory, name, version string) ([]byte, error) {
	tree := root
	if directory != "." {
		var err error
		tree, err = root.Tree(directory)
		if err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp("", "aif-chart-package-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	chartDir := filepath.Join(dir, "source")
	if err := os.Mkdir(chartDir, 0700); err != nil {
		return nil, err
	}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	var total int64
	for count := 0; ; count++ {
		filename, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if count >= 10000 {
			return nil, fmt.Errorf("%w: more than 10000 entries", ErrChartTooLarge)
		}
		if entry.Mode == filemode.Dir {
			continue
		}
		if path.IsAbs(filename) || path.Clean(filename) != filename || strings.HasPrefix(filename, "../") || strings.Contains(filename, "\\") {
			return nil, fmt.Errorf("Git chart contains an unsafe path")
		}
		data, err := readGitTreeEntry(walker.Tree(), &entry, filename, maxGitChartUnpackedBytes-total)
		if err != nil {
			return nil, err
		}
		total += int64(len(data))
		dest := filepath.Join(chartDir, filepath.FromSlash(filename))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			return nil, err
		}
	}
	// Use Helm's packaging rules, including .helmignore and bundled dependencies.
	// This does not run dependency update or contact any chart repository.
	chart, err := loader.LoadDir(chartDir)
	if err != nil {
		return nil, fmt.Errorf("load Git chart directory: %w", err)
	}
	if chart.Metadata.Name != name || chart.Metadata.Version != version {
		return nil, fmt.Errorf("Git chart metadata does not match requested name/version")
	}
	archivePath, err := chartutil.Save(chart, dir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	archive, err := io.ReadAll(io.LimitReader(f, maxGitChartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(archive) > maxGitChartBytes {
		return nil, ErrChartTooLarge
	}
	return archive, validateGitChart(archive, name, version)
}
