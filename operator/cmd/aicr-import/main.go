// Command aicr-import converts an NVIDIA AI Cluster Runtime (AICR) bundle into
// an AI Factory blueprint catalog.
//
//	aicr bundle -r recipe.yaml -o ./bundle
//	aicr-import --bundle ./bundle --out ./catalog \
//	    --name nvidia-gpu-platform --display-name "NVIDIA GPU Platform" --version 1.0.0
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/SUSE/aif-operator/internal/aicrimport"
)

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}

func main() {
	var (
		bundle, out string
		opts        aicrimport.Options
		only, repos listFlag
	)
	flag.StringVar(&bundle, "bundle", "", "AICR bundle directory (output of `aicr bundle`)")
	flag.StringVar(&out, "out", "", "output catalog directory")
	flag.StringVar(&opts.Name, "name", "", "blueprint family slug (DNS-1123 label)")
	flag.StringVar(&opts.DisplayName, "display-name", "", "blueprint display name")
	flag.StringVar(&opts.Version, "version", "", "blueprint version (SemVer)")
	flag.StringVar(&opts.Description, "description", "", "text prepended to the generated description")
	flag.StringVar(&opts.Catalog, "catalog", "nvidia-aicr", "catalog name (Settings blueprintCatalogs entry)")
	flag.StringVar(&opts.CatalogDisplayName, "catalog-display-name", "NVIDIA AICR", "catalog display name")
	flag.BoolVar(&opts.Single, "single", false,
		"one blueprint instead of tiers (only when no selected component depends on another)")
	flag.Var(&only, "only", "export only these components (repeatable or comma-separated)")
	flag.Var(&repos, "repo", "reuse an existing repo for a source: URL=NAME (repeatable)")
	flag.Parse()

	if bundle == "" || out == "" {
		fmt.Fprintln(os.Stderr, "--bundle and --out are required")
		flag.Usage()
		os.Exit(2)
	}
	opts.Only = only
	opts.RepoNames = map[string]string{}
	for _, r := range repos {
		u, n, ok := strings.Cut(r, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "--repo %q: want URL=NAME\n", r)
			os.Exit(2)
		}
		opts.RepoNames[u] = n
	}

	b, err := aicrimport.ReadBundle(bundle)
	if err != nil {
		fatal(err)
	}
	res, err := aicrimport.Plan(b, opts)
	if err != nil {
		fatal(err)
	}
	if err := aicrimport.Write(res, out); err != nil {
		fatal(err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	fmt.Printf("wrote %d blueprint(s), %d custom repo(s) to %s\n", len(res.Blueprints), len(res.CustomRepos), out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
