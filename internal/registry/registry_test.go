package registry

import (
	"strings"
	"testing"
)

func TestParseRoute(t *testing.T) {
	d := "sha256:" + strings.Repeat("a", 64)
	cases := map[string]Route{
		"/v2":                                    {Kind: RouteBase},
		"/v2/":                                   {Kind: RouteBase},
		"/v2/_catalog":                           {Kind: RouteCatalog},
		"/v2/org/game/tags/list":                 {Kind: RouteTags, Name: "org/game"},
		"/v2/org/game/issues/blobs/uploads/":     {Kind: RouteUploads, Name: "org/game/issues"},
		"/v2/org/game/blobs/uploads":             {Kind: RouteUploads, Name: "org/game"},
		"/v2/org/game/blobs/uploads/abc-123":     {Kind: RouteUpload, Name: "org/game", Ref: "abc-123"},
		"/v2/org/game/blobs/" + d:                {Kind: RouteBlob, Name: "org/game", Ref: d},
		"/v2/org/game/a/b/manifests/v1.2":        {Kind: RouteManifest, Name: "org/game/a/b", Ref: "v1.2"},
		"/v2/org/game/referrers/" + d:            {Kind: RouteReferrers, Name: "org/game", Ref: d},
		"/v2/org/manifests/manifests/latest":     {Kind: RouteManifest, Name: "org/manifests", Ref: "latest"},
		"/v2/org/game/blobs/uploads/manifests/x": {Kind: RouteManifest, Name: "org/game/blobs/uploads", Ref: "x"},
	}
	for p, want := range cases {
		got, ok := ParseRoute(p)
		if !ok || got != want {
			t.Errorf("%s: got %+v %v, want %+v", p, got, ok, want)
		}
	}
	for _, p := range []string{"/v3/x", "/v2/org/game", "/v2/org/game/manifests/", "/v2x"} {
		if r, ok := ParseRoute(p); ok {
			t.Errorf("%s parsed as %+v", p, r)
		}
	}
}

func TestNames(t *testing.T) {
	for _, n := range []string{"bloxblast/game", "a/b/c-d", "x/y__z/w.v", "a/b--c"} {
		if !ValidName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"Bloxblast/game", "a//b", "a/-b", "a/b_", "a/._b", ""} {
		if ValidName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
	if got := Namespace("Blox Blast, Inc."); got != "blox-blast-inc" {
		t.Errorf("namespace: %q", got)
	}
	if got := Namespace("!!!"); got != "org" {
		t.Errorf("empty namespace: %q", got)
	}
	if got := ImagePath("BloxBlast", "BloxBlast", "issues"); got != "bloxblast/bloxblast/issues" {
		t.Errorf("image path: %q", got)
	}
	if !ValidTag("g1a2b3c4d5e6f") || !ValidTag("master") || ValidTag(".x") || ValidTag(strings.Repeat("a", 129)) {
		t.Error("tags")
	}
}

func TestParseManifest(t *testing.T) {
	d := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	img := `{"schemaVersion":2,"mediaType":"` + MediaDockerManifest + `","config":{"mediaType":"x","digest":"` + d("a") + `","size":10},
	  "layers":[{"mediaType":"l","digest":"` + d("b") + `","size":20},{"mediaType":"f","digest":"` + d("c") + `","size":5,"urls":["https://x"]}]}`
	m, mt, err := ParseManifest(MediaDockerManifest, []byte(img))
	if err != nil || mt != MediaDockerManifest {
		t.Fatalf("%v %s", err, mt)
	}
	if b := m.Blobs(); len(b) != 2 || b[0] != d("a") || b[1] != d("b") || m.Size() != 35 {
		t.Fatalf("blobs %v size %d", b, m.Size())
	}
	// mediaType from the body when Content-Type is missing; OCI shape detection.
	if _, mt, err := ParseManifest("", []byte(`{"schemaVersion":2,"manifests":[{"mediaType":"x","digest":"`+d("a")+`","size":1}]}`)); err != nil || mt != MediaOCIIndex {
		t.Fatalf("index: %v %s", err, mt)
	}
	bad := map[string]string{
		`{`: "valid JSON",
		`{"schemaVersion":1,"mediaType":"application/vnd.docker.distribution.manifest.v1+json"}`:       "isn't supported",
		`{"schemaVersion":2,"mediaType":"` + MediaOCIManifest + `"}`:                                   "needs a config",
		`{"schemaVersion":2,"mediaType":"` + MediaOCIManifest + `","config":{"digest":"md5:x"}}`:       "invalid or unsupported digest",
		`{"schemaVersion":2,"mediaType":"` + MediaOCIIndex + `","config":{"digest":"` + d("a") + `"}}`: "can't have config",
	}
	for body, want := range bad {
		if _, _, err := ParseManifest("", []byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", body, err, want)
		}
	}
	if _, _, err := ParseManifest(MediaOCIIndex, []byte(img)); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Errorf("mismatched type: %v", err)
	}
}
