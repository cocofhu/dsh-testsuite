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
	if len(cat.Releases) != 5 {
		t.Fatalf("releases=%+v", cat.Releases)
	}
	rc12, rc11, rc8, rc7, rc6 := cat.Releases[0], cat.Releases[1], cat.Releases[2], cat.Releases[3], cat.Releases[4]
	if rc12.Version != "0.1.1-rc.2" || rc12.Registered || rc12.Present {
		t.Fatalf("rc12=%+v", rc12)
	}
	if rc11.Version != "0.1.1-rc.1" || rc11.Registered || rc11.Present {
		t.Fatalf("rc11=%+v", rc11)
	}
	if rc8.Version != "0.1.0-rc.8" || !rc8.Registered || !rc8.Present {
		t.Fatalf("rc8=%+v", rc8)
	}
	if rc8.Ref != "dsh-testsuite-runtime:0.1.0-rc.8" {
		t.Fatalf("ref=%q", rc8.Ref)
	}
	if rc7.Version != "0.1.0-rc.7" || rc7.Registered || rc7.Present {
		t.Fatalf("rc7=%+v", rc7)
	}
	if rc6.Version != "0.1.0-rc.6" || rc6.Registered || rc6.Present {
		t.Fatalf("rc6=%+v", rc6)
	}
}
