package attest

import "testing"

// A page whose recorded input hash equals the live chain hash is attested —
// green, rev-compare only, no ^check run (RunCheck is never wired here).
func TestDriftGreenAttested(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: ownedPage("PLACEHOLDER")})
	// Re-seed with the live hash so recorded == live.
	f.eng.Raw[rel] = []byte(ownedPage(depHash(t, f)))

	rep, err := f.eng.Drift(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	d := oneDrift(t, rep)
	if d.Color != ColorGreen || d.State != DriftAttested {
		t.Fatalf("want green/attested, got %s/%s (reason=%q)", d.Color, d.State, d.Reason)
	}
	if *f.wrote {
		t.Fatal("status is a pure read — it must never write")
	}
}

// A recorded hash that no longer matches the live chain is drift — red, with the
// drifted input ref surfaced and no write.
func TestDriftRedDrifted(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: ownedPage(string(hexHash("stale-recorded")))})

	rep, err := f.eng.Drift(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	d := oneDrift(t, rep)
	if d.Color != ColorRed || d.State != DriftDrifted {
		t.Fatalf("want red/drifted, got %s/%s (reason=%q)", d.Color, d.State, d.Reason)
	}
	if len(d.DriftedRefs) != 1 || d.DriftedRefs[0] != "[[dep#Sec]]" {
		t.Fatalf("want drifted ref [[dep#Sec]], got %v", d.DriftedRefs)
	}
	if *f.wrote {
		t.Fatal("status is a pure read — it must never write")
	}
}

// A page outside the ledger's sight (not type/effect) renders grey/unmanaged —
// never attested. This is the honesty floor: absence of knowledge must not read
// as verified-true.
func TestDriftGreyUnmanagedNonEffect(t *testing.T) {
	rel := "wiki/plain.md"
	plain := "---\ntitle: just a note\ntags: [type/note]\n---\n\n# Plain\n\nprose\n"
	f := newFixture(t, map[string]string{rel: plain})

	rep, err := f.eng.Drift(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	d := oneDrift(t, rep)
	if d.Color != ColorGrey || d.State != DriftUnmanaged {
		t.Fatalf("want grey/unmanaged, got %s/%s", d.Color, d.State)
	}
	if d.State == DriftAttested {
		t.Fatal("an unmanaged page must never read as attested")
	}
}

// An effect page that was never attested (no recorded input hashes) is grey —
// declared, but no recorded truth to compare against.
func TestDriftGreyNeverAttested(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: ownedPage("null")})

	rep, err := f.eng.Drift(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	d := oneDrift(t, rep)
	if d.Color != ColorGrey || d.State != DriftUnmanaged {
		t.Fatalf("want grey/unmanaged (never attested), got %s/%s", d.Color, d.State)
	}
}

// A typo'd page is an invocation error — it must never read as a clean green.
func TestDriftUnknownPageErrors(t *testing.T) {
	f := newFixture(t, map[string]string{"effects/skills/caveman.md": ownedPage("null")})
	if _, err := f.eng.Drift(Options{Page: "effects/skills/nope.md"}); err == nil {
		t.Fatal("want error for a page not in the corpus")
	}
}

func oneDrift(t *testing.T, rep *DriftReport) PageDrift {
	t.Helper()
	if len(rep.Pages) != 1 {
		t.Fatalf("want 1 drift row, got %+v", rep.Pages)
	}
	return rep.Pages[0]
}
