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
	"encoding/json"
	"strings"
)

// convergedAt records that a spec was proven to need no upgrade, and against
// which deployed revision that was proven.
type convergedAt struct {
	revision int
	spec     string
}

// EnsureRelease reaches the manifest diff only when the stored release disagrees
// with the spec on version or values. The diff can then find that the
// disagreement is cosmetic — the chart renders exactly what is already running —
// and skip the upgrade. Nothing is written back when it does, because there is
// nothing to write: the release is already correct. So the next reconcile finds
// the same disagreement, pulls the chart to render the same manifest, reaches the
// same conclusion, and pulls again 60 seconds later. The operator converges on
// every pass and has no way to record that it did.
//
// Two states land a release there, and neither resolves on its own:
//
//   - The chart's own Chart.yaml version differs from the tag the CR pins. Helm
//     stores the chart's version, never the requested one, so no upgrade can make
//     releaseNeedsUpgrade's version comparison come out equal. Today aif-ui's
//     chart version and its tag happen to match, so this is dormant rather than
//     fixed — nothing enforces it, and a mismatch is invisible in the logs, which
//     report a cheerful "up-to-date, skipping upgrade" on every pass.
//   - The CR carries a values key the chart never references. The rendered
//     manifest is identical, so the upgrade that would have written those values
//     into storage is skipped, so storage keeps disagreeing.
//
// The latch memoizes the verdict instead of re-deriving it. It is keyed on the
// deployed revision, so anything that changes the release — an upgrade, a
// rollback, a human running helm by hand — invalidates it, and on a fingerprint
// of everything in the spec that can change what the chart renders, so any edit
// to the CR does too.
//
// This does not weaken drift detection, which is worth stating because it looks
// like it should. The diff being memoized compares the rendered manifest against
// the manifest Helm *stored*, not against the live cluster, so it never detected
// hand-edited resources to begin with. Nor does it hide a chart re-pushed under
// the same mutable tag: that is already invisible, because a release whose
// version and values both match the spec takes decideRelease's actionSkip path
// and never pulls at all. The latch only engages where that fast path did not.
//
// In-memory, so a restart costs one extra pull per release. That is the right
// trade against persisting it: the verdict is cheap to re-derive once and stale
// state on the CR would be worse than no state.
func (c *helmClient) convergenceHolds(spec ReleaseSpec, deployed *ReleaseInfo) bool {
	if deployed == nil {
		return false
	}
	entry, ok := c.converged.Load(spec.Name)
	if !ok {
		return false
	}
	fingerprint, ok := specFingerprint(spec)
	if !ok {
		return false
	}
	at, ok := entry.(convergedAt)
	return ok && at.revision == deployed.Revision && at.spec == fingerprint
}

func (c *helmClient) latchConvergence(spec ReleaseSpec, deployed *ReleaseInfo) {
	if deployed == nil {
		return
	}
	fingerprint, ok := specFingerprint(spec)
	if !ok {
		return
	}
	c.converged.Store(spec.Name, convergedAt{revision: deployed.Revision, spec: fingerprint})
}

func (c *helmClient) dropConvergence(name string) {
	c.converged.Delete(name)
}

// specFingerprint captures everything about a spec that can change what the
// chart renders. Credentials and TLS are excluded deliberately: they decide
// whether the pull succeeds, not what comes back.
//
// Returns false when the values cannot be marshalled, which callers treat as
// "cannot latch" rather than as an empty fingerprint — the latter would compare
// equal to another unmarshallable spec and skip an upgrade that was never
// verified.
func specFingerprint(spec ReleaseSpec) (string, bool) {
	values, ok := valuesFingerprint(spec.Values)
	if !ok {
		return "", false
	}
	return strings.Join([]string{spec.ChartRef, spec.RepoURL, spec.Version, values}, "\x00"), true
}

// valuesFingerprint mirrors valuesEqual's comparison — json.Marshal sorts map
// keys, so the encoding is stable — so that two specs the latch calls equal are
// exactly the two specs releaseNeedsUpgrade would call equal.
func valuesFingerprint(values map[string]interface{}) (string, bool) {
	if len(values) == 0 {
		return "", true
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}
