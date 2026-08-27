package aiworkload

import (
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestPruneRenderBaselines(t *testing.T) {
	in := []aiplatformv1alpha1.RenderBaseline{
		{HelmOpUID: "b"}, {HelmOpUID: "gone"}, {HelmOpUID: "a"},
	}
	out := pruneRenderBaselines(in, map[string]bool{"a": true, "b": true})
	if len(out) != 2 || out[0].HelmOpUID != "a" || out[1].HelmOpUID != "b" {
		t.Fatalf("want sorted [a,b], got %+v", out)
	}
}
