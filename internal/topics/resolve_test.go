package topics

import "testing"

func slugs(binds []Bind) []string {
	out := make([]string, len(binds))
	for i, b := range binds {
		out[i] = b.Slug
	}
	return out
}

func hasSlug(binds []Bind, slug string) bool {
	for _, b := range binds {
		if b.Slug == slug {
			return true
		}
	}
	return false
}

func TestResolveFlockCamerasAlias(t *testing.T) {
	cat := newMem("flock-alpr", "Flock / ALPR")
	got := Resolve(Window{
		Title:  "The Flawed 'Opt-Out' Argument for Flock Cameras",
		Topics: []string{"Flock Cameras", "Phone Privacy", "Opt-Out Mechanisms", "Contrarian Arguments"},
	}, cat)
	if !hasSlug(got, "flock-alpr") {
		t.Fatalf("expected flock-alpr, got %v", slugs(got))
	}
	if hasSlug(got, "opt-out-mechanisms") || hasSlug(got, "contrarian-arguments") || hasSlug(got, "minutes-approval") {
		t.Fatalf("procedural leaked: %v", slugs(got))
	}
}

func TestResolveFlockEntityOnly(t *testing.T) {
	cat := newMem("flock-alpr", "Flock / ALPR")
	got := Resolve(Window{
		Title:    "Meeting Start & Flock Investigation Vote",
		Topics:   []string{"Technical Setup", "Minutes Approval", "Executive Session Release", "Committee of the Whole Motion"},
		Entities: []string{"Alderman Palachek", "Flock", "Gerulius"},
	}, cat)
	if !hasSlug(got, "flock-alpr") {
		t.Fatalf("entity Flock should bind flock-alpr, got %v", slugs(got))
	}
	if hasSlug(got, "minutes-approval") || hasSlug(got, "technical-setup") {
		t.Fatalf("procedural leaked: %v", slugs(got))
	}
	if hasSlug(got, "alderman-palachek") || hasSlug(got, "gerulius") {
		t.Fatalf("unaliased entity created a topic: %v", slugs(got))
	}
}

func TestResolveFlockSurveillanceParaphrase(t *testing.T) {
	cat := newMem("flock-alpr", "Flock / ALPR")
	got := Resolve(Window{
		Title:  "Public Forum: Community Outcry",
		Topics: []string{"Public Comment", "Flock Surveillance", "Committee Criticism", "Contract Breach"},
	}, cat)
	if !hasSlug(got, "flock-alpr") {
		t.Fatalf("Flock Surveillance should alias to flock-alpr, got %v", slugs(got))
	}
	if hasSlug(got, "public-comment") || hasSlug(got, "committee-criticism") {
		t.Fatalf("procedural leaked: %v", slugs(got))
	}
}

func TestResolveTitleTokenWithoutTopics(t *testing.T) {
	cat := newMem("flock-alpr", "Flock / ALPR")
	got := Resolve(Window{Title: "Meeting Start & Flock Investigation Vote"}, cat)
	if !hasSlug(got, "flock-alpr") {
		t.Fatalf("title token Flock should bind, got %v", slugs(got))
	}
}

func TestResolveUnseededCreatesDurableTopic(t *testing.T) {
	cat := newMem("", "")
	got := Resolve(Window{Topics: []string{"Flock Cameras"}}, cat)
	if !hasSlug(got, "flock-cameras") {
		t.Fatalf("unseeded durable label should catalog, got %v", slugs(got))
	}
	if cat.origin["flock-cameras"] != "resolver" {
		t.Fatalf("origin %q", cat.origin["flock-cameras"])
	}
}

func TestResolveDoesNotCreateFromEntity(t *testing.T) {
	cat := newMem("", "")
	got := Resolve(Window{Entities: []string{"Flock", "ICE"}}, cat)
	if len(got) != 0 {
		t.Fatalf("entities must not create topics, got %v", slugs(got))
	}
}

func TestIsProcedural(t *testing.T) {
	for _, s := range []string{"Minutes Approval", "Technical Setup", "Public Comment", "Opt-Out Mechanisms", "Personal Growth"} {
		if !IsProcedural(s) {
			t.Fatalf("%q should be procedural", s)
		}
	}
	for _, s := range []string{"Flock Cameras", "Consent", "Fourth Amendment", "Data Privacy"} {
		if IsProcedural(s) {
			t.Fatalf("%q should be durable", s)
		}
	}
}
