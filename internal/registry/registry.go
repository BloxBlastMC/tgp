// Package registry holds the protocol pieces of tgp's container registry
// (OCI Distribution v2): route and name parsing, digests, manifest parsing
// and the error envelope. The HTTP handlers, storage and database live in
// the server package.
//
// Images are addressed as <host>/<namespace>/<repo>[/<name>]:<tag>, where
// namespace is the organisation's name as a slug and repo a tgp
// repository; access follows the repository's permissions.
package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Media types of manifests the registry accepts.
const (
	MediaOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	MediaOCIIndex       = "application/vnd.oci.image.index.v1+json"
	MediaDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	MediaDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// MaxManifestBytes is the largest manifest accepted (the spec's
// recommended minimum limit).
const MaxManifestBytes = 4 << 20

// IsIndex reports whether a manifest media type lists other manifests.
func IsIndex(mediaType string) bool {
	return mediaType == MediaOCIIndex || mediaType == MediaDockerList
}

// Supported reports whether the registry stores manifests of this type.
func Supported(mediaType string) bool {
	switch mediaType {
	case MediaOCIManifest, MediaOCIIndex, MediaDockerManifest, MediaDockerList:
		return true
	}
	return false
}

var (
	// component is one path component of a repository name (OCI grammar).
	component = `[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*`
	nameRe    = regexp.MustCompile(`^` + component + `(?:/` + component + `)*$`)
	tagRe     = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	digestRe  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	slugBadRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidName reports whether name is a valid repository name.
func ValidName(name string) bool { return len(name) <= 255 && nameRe.MatchString(name) }

// ValidTag reports whether s is a valid tag.
func ValidTag(s string) bool { return tagRe.MatchString(s) }

// ValidDigest reports whether s is a sha256 digest (the only algorithm
// the registry accepts).
func ValidDigest(s string) bool { return digestRe.MatchString(s) }

// Namespace turns the organisation's display name into the first path
// component of image names: lowercase, with runs of other characters
// replaced by "-".
func Namespace(org string) string {
	s := strings.Trim(slugBadRe.ReplaceAllString(strings.ToLower(org), "-"), "-")
	if s == "" {
		return "org"
	}
	return s
}

// ImagePath is <namespace>/<repo>[/<name>], lowercased.
func ImagePath(org, repo, name string) string {
	p := Namespace(org) + "/" + strings.ToLower(repo)
	if name != "" {
		p += "/" + name
	}
	return p
}

// Route kinds.
const (
	RouteBase      = "base"      // /v2/
	RouteCatalog   = "catalog"   // /v2/_catalog
	RouteBlob      = "blob"      // /v2/<name>/blobs/<digest>
	RouteUploads   = "uploads"   // /v2/<name>/blobs/uploads/
	RouteUpload    = "upload"    // /v2/<name>/blobs/uploads/<id>
	RouteManifest  = "manifest"  // /v2/<name>/manifests/<reference>
	RouteTags      = "tags"      // /v2/<name>/tags/list
	RouteReferrers = "referrers" // /v2/<name>/referrers/<digest>
)

// Route is a parsed registry URL path.
type Route struct {
	Kind string
	Name string // repository name (namespace/repo[/image])
	Ref  string // digest, tag or upload id
}

// ParseRoute parses a path under /v2/. ok is false for paths that aren't
// registry endpoints.
func ParseRoute(path string) (Route, bool) {
	p, ok := strings.CutPrefix(path, "/v2")
	if !ok {
		return Route{}, false
	}
	if p == "" || p == "/" {
		return Route{Kind: RouteBase}, true
	}
	p = strings.TrimPrefix(p, "/")
	if p == "_catalog" {
		return Route{Kind: RouteCatalog}, true
	}
	if name, ok := strings.CutSuffix(p, "/tags/list"); ok {
		return Route{Kind: RouteTags, Name: name}, true
	}
	if name, ok := strings.CutSuffix(p, "/blobs/uploads/"); ok {
		return Route{Kind: RouteUploads, Name: name}, true
	}
	if name, ok := strings.CutSuffix(p, "/blobs/uploads"); ok {
		return Route{Kind: RouteUploads, Name: name}, true
	}
	for _, r := range []struct{ sep, kind string }{
		{"/blobs/uploads/", RouteUpload},
		{"/manifests/", RouteManifest},
		{"/blobs/", RouteBlob},
		{"/referrers/", RouteReferrers},
	} {
		if i := strings.LastIndex(p, r.sep); i > 0 {
			ref := p[i+len(r.sep):]
			if ref == "" || strings.Contains(ref, "/") {
				continue
			}
			return Route{Kind: r.kind, Name: p[:i], Ref: ref}, true
		}
	}
	return Route{}, false
}

// Descriptor points at a blob or manifest.
type Descriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	URLs         []string          `json:"urls,omitempty"`
	Platform     *Platform         `json:"platform,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	ArtifactType string            `json:"artifactType,omitempty"`
}

// Platform is an index entry's platform.
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// String is os/arch[/variant].
func (p *Platform) String() string {
	if p == nil {
		return ""
	}
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}

// Manifest is an image manifest or an index, as far as the registry cares.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        *Descriptor       `json:"config,omitempty"`
	Layers        []Descriptor      `json:"layers,omitempty"`
	Manifests     []Descriptor      `json:"manifests,omitempty"`
	Subject       *Descriptor       `json:"subject,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Blobs are the blobs an image manifest needs in the repository (config
// and layers; foreign layers with URLs are fetched elsewhere).
func (m *Manifest) Blobs() []string {
	var out []string
	if m.Config != nil {
		out = append(out, m.Config.Digest)
	}
	for _, l := range m.Layers {
		if len(l.URLs) == 0 {
			out = append(out, l.Digest)
		}
	}
	return out
}

// Size is the total size of an image manifest's config and layers.
func (m *Manifest) Size() int64 {
	var n int64
	if m.Config != nil {
		n += m.Config.Size
	}
	for _, l := range m.Layers {
		n += l.Size
	}
	return n
}

// ParseManifest checks a manifest body. contentType is the request's
// Content-Type; when empty the body's mediaType is used. It returns the
// manifest and its media type.
func ParseManifest(contentType string, body []byte) (*Manifest, string, error) {
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, "", fmt.Errorf("the manifest isn't valid JSON: %v", err)
	}
	mt := strings.TrimSpace(strings.Split(contentType, ";")[0])
	if mt == "" || mt == "application/json" || mt == "application/octet-stream" {
		mt = m.MediaType
	}
	if mt == "" {
		// OCI manifests may leave out mediaType; tell them apart by shape.
		if m.Manifests != nil {
			mt = MediaOCIIndex
		} else {
			mt = MediaOCIManifest
		}
	}
	if !Supported(mt) {
		return nil, "", fmt.Errorf("manifest type %q isn't supported (use OCI or Docker schema 2 manifests)", mt)
	}
	if m.MediaType != "" && m.MediaType != mt {
		return nil, "", fmt.Errorf("the manifest's mediaType %q doesn't match its Content-Type %q", m.MediaType, mt)
	}
	if m.SchemaVersion != 2 {
		return nil, "", fmt.Errorf("schemaVersion must be 2")
	}
	check := func(d Descriptor, what string) error {
		if !ValidDigest(d.Digest) {
			return fmt.Errorf("%s has an invalid or unsupported digest %q (sha256 only)", what, d.Digest)
		}
		if d.Size < 0 {
			return fmt.Errorf("%s has a negative size", what)
		}
		return nil
	}
	if IsIndex(mt) {
		if m.Config != nil || len(m.Layers) > 0 {
			return nil, "", fmt.Errorf("an index can't have config or layers")
		}
		for i, d := range m.Manifests {
			if err := check(d, fmt.Sprintf("manifests[%d]", i)); err != nil {
				return nil, "", err
			}
		}
	} else {
		if m.Config == nil {
			return nil, "", fmt.Errorf("an image manifest needs a config")
		}
		if err := check(*m.Config, "config"); err != nil {
			return nil, "", err
		}
		for i, d := range m.Layers {
			if err := check(d, fmt.Sprintf("layers[%d]", i)); err != nil {
				return nil, "", err
			}
		}
		if len(m.Manifests) > 0 {
			return nil, "", fmt.Errorf("an image manifest can't list manifests")
		}
	}
	if m.Subject != nil {
		if err := check(*m.Subject, "subject"); err != nil {
			return nil, "", err
		}
	}
	return &m, mt, nil
}

// Error codes from the distribution spec.
const (
	CodeBlobUnknown         = "BLOB_UNKNOWN"
	CodeBlobUploadInvalid   = "BLOB_UPLOAD_INVALID"
	CodeBlobUploadUnknown   = "BLOB_UPLOAD_UNKNOWN"
	CodeDigestInvalid       = "DIGEST_INVALID"
	CodeManifestBlobUnknown = "MANIFEST_BLOB_UNKNOWN"
	CodeManifestInvalid     = "MANIFEST_INVALID"
	CodeManifestUnknown     = "MANIFEST_UNKNOWN"
	CodeNameInvalid         = "NAME_INVALID"
	CodeNameUnknown         = "NAME_UNKNOWN"
	CodeSizeInvalid         = "SIZE_INVALID"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeDenied              = "DENIED"
	CodeUnsupported         = "UNSUPPORTED"
	CodeTooManyRequests     = "TOOMANYREQUESTS"
	CodeRangeInvalid        = "RANGE_INVALID"
)

// Error is a registry error with its HTTP status.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf makes an Error.
func Errorf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Write sends the spec's error envelope. HEAD responses get no body.
func (e *Error) Write(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"code": e.Code, "message": e.Message}}})
}
