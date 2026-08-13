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
	"time"
)

// convergenceTTL bounds how long a memoized verdict is trusted before the chart
// is pulled and the diff derived again.
//
// Every other thing that can falsify the verdict can also be observed: the CR is
// read on every pass, and so is the stored release, so an edit to either shows up
// in a fingerprint and invalidates the entry. A chart re-pushed under a tag
// already in use is the exception. It changes what the chart renders while every
// input the latch keys on stays byte-identical, so no invalidation can fire on
// it. Only time can.
//
// Thirty minutes against a sixty-second reconcile removes twenty-nine pulls in
// every thirty, which is essentially the whole win, and bounds the window in
// which a re-pushed chart goes unapplied to the next half hour rather than to
// whenever the operator next restarts.
const convergenceTTL = 30 * time.Minute

// convergedAt records that a spec was proven to need no upgrade, and against
// which deployed release that was proven.
type convergedAt struct {
	revision int
	spec     string
	// provenAt is when the diff that produced this verdict actually ran, so the
	// verdict can be aged out. See convergenceTTL.
	provenAt time.Time
	// deployed fingerprints the stored release the verdict was proven against.
	// The revision number alone does not identify it: uninstalling a release and
	// installing another under the same name starts again at revision 1, so
	// without this a verdict would go on suppressing upgrades for a release it
	// had never seen.
	deployed string
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
//   - The stored values and the requested values disagree, in either direction,
//     in a way no upgrade removes. A key only the CR carries is one the chart
//     never references: the rendered manifest is identical, so the upgrade that
//     would have written it into storage is skipped, so storage keeps
//     disagreeing. A key only storage carries survives for a different reason —
//     Helm's reuseValues (pkg/action/upgrade.go) copies the previous release's
//     config forward whenever the new values are empty, and this client sets
//     none of ResetValues, ReuseValues or ResetThenReuseValues. So a CR that
//     declares no values cannot clear one that is already stored, and cannot
//     clear it by upgrading either: the dry-run render is given the same
//     copied-forward values, which is why the manifest comes out identical.
//
// The latch memoizes the verdict instead of re-deriving it. It is keyed on three
// things, each closing off a way the verdict could outlive what it was proven
// against: the deployed revision, so an upgrade or a rollback invalidates it; a
// fingerprint of the stored release, so a release uninstalled and reinstalled
// under the same name does too — which the revision alone misses, because a
// reinstall starts again at revision 1; and a fingerprint of everything in the
// spec that can change what the chart renders, so any edit to the CR does too.
// It also expires, for the one falsifier no key can cover: see convergenceTTL.
//
// This does not weaken drift detection against the live cluster, which is worth
// stating because it looks like it should. The diff being memoized compares the
// rendered manifest against the manifest Helm *stored*, not against the cluster,
// so it never detected hand-edited resources to begin with.
//
// It would have weakened detection of a chart re-pushed under a tag already in
// use, and this is worth spelling out because the obvious argument that it does
// not is wrong. That argument runs: such a re-push is already invisible, because
// a release whose version and values match its spec takes decideRelease's
// actionSkip path and never pulls. True of the fast path — and the latch engages
// on precisely the complement of it. The releases that reach here are the ones
// that did pull and render on every pass, and so were the only ones that noticed
// when the bytes behind a tag changed. Memoizing their verdict for the life of
// the process would have bought one pull a minute at the price of a re-pushed
// chart that is never applied at all. The TTL is what makes that a bounded delay
// instead.
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
	stored, ok := deployedFingerprint(deployed)
	if !ok {
		return false
	}
	at, ok := entry.(convergedAt)
	return ok &&
		at.revision == deployed.Revision &&
		at.spec == fingerprint &&
		at.deployed == stored &&
		time.Since(at.provenAt) <= convergenceTTL
}

func (c *helmClient) latchConvergence(spec ReleaseSpec, deployed *ReleaseInfo) {
	if deployed == nil {
		return
	}
	fingerprint, ok := specFingerprint(spec)
	if !ok {
		return
	}
	stored, ok := deployedFingerprint(deployed)
	if !ok {
		return
	}
	c.converged.Store(spec.Name, convergedAt{
		revision: deployed.Revision,
		spec:     fingerprint,
		deployed: stored,
		provenAt: time.Now(),
	})
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

// deployedFingerprint identifies the stored release a verdict was proven
// against, so that a release replaced out of band is not mistaken for the one
// the operator verified.
//
// Chart name, chart version and stored values, because those are exactly what
// the memoized diff concluded something about: that this chart, at this version,
// with these values, renders what is already running. Not the stored manifest,
// which would be the most direct thing to compare but costs a second read of
// release storage on every reconcile — the very kind of per-pass work the latch
// exists to remove.
//
// Returns false for the same reason specFingerprint does, and callers treat it
// the same way: no fingerprint means no latch, which costs a render rather than
// risking a skipped upgrade.
func deployedFingerprint(deployed *ReleaseInfo) (string, bool) {
	values, ok := valuesFingerprint(deployed.Values)
	if !ok {
		return "", false
	}
	return strings.Join([]string{deployed.ChartName, deployed.Version, values}, "\x00"), true
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
