package env

import (
	"context"
	"testing"
)

func TestListRemoteImages(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.8"] = true
	s := testService(t, fake)
	if _, err := s.UpsertImage(ImageConfig{Version: "0.1.0-rc.8"}); err != nil {
		t.Fatal(err)
	}

	cat, err := s.ListRemoteImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cat.ImageRepo != "dsh-testsuite-runtime" {
		t.Fatalf("catalog=%+v", cat)
	}
	// All npm-published @deepseek-ai/dsh versions, newest first.
	wantOrder := []string{
		"0.1.6-alpha.1",
		"0.1.5-rc.2",
		"0.1.5-rc.1",
		"0.1.5-alpha.2",
		"0.1.5-alpha.1",
		"0.1.3-alpha.2",
		"0.1.2-rc.1",
		"0.1.2-alpha.5",
		"0.1.2-alpha.4",
		"0.1.2-alpha.3",
		"0.1.2-alpha.2",
		"0.1.1-rc.2",
		"0.1.1-rc.1",
		"0.1.0-rc.8",
		"0.1.0-rc.7",
		"0.1.0-rc.6",
		"0.1.0-rc.3",
		"0.1.0-rc.2",
		"0.0.1-rc.5",
		"0.0.1-rc.2",
		"0.0.1-rc.1",
	}
	if len(cat.Releases) != len(wantOrder) {
		t.Fatalf("releases=%d want %d: %+v", len(cat.Releases), len(wantOrder), cat.Releases)
	}
	for i, want := range wantOrder {
		got := cat.Releases[i]
		if got.Version != want {
			t.Fatalf("release[%d].Version=%q want %q", i, got.Version, want)
		}
		if i > 0 && cat.Releases[i-1].Version < got.Version {
			t.Fatalf("releases not sorted descending at %d: %q < %q", i, cat.Releases[i-1].Version, got.Version)
		}
	}

	rc8 := cat.Releases[13]
	if rc8.Version != "0.1.0-rc.8" || !rc8.Registered || !rc8.Present {
		t.Fatalf("rc8=%+v", rc8)
	}
	if rc8.Ref != "dsh-testsuite-runtime:0.1.0-rc.8" {
		t.Fatalf("ref=%q", rc8.Ref)
	}
	rc7 := cat.Releases[14]
	if rc7.Version != "0.1.0-rc.7" || rc7.Registered || rc7.Present {
		t.Fatalf("rc7=%+v", rc7)
	}
	for i, rel := range cat.Releases {
		if i == 13 {
			continue
		}
		if rel.Registered || rel.Present {
			t.Fatalf("unexpected registered/present at %d: %+v", i, rel)
		}
	}
}
