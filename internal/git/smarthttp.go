package git

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

// Service is a smart HTTP service.
type Service string

const (
	UploadPack  Service = "git-upload-pack"  // fetch / clone
	ReceivePack Service = "git-receive-pack" // push
)

// ParseService validates a ?service= value.
func ParseService(s string) (Service, bool) {
	switch Service(s) {
	case UploadPack, ReceivePack:
		return Service(s), true
	}
	return "", false
}

// RPCOptions configures one smart HTTP exchange.
type RPCOptions struct {
	// Env is added to the git process environment (hook context).
	Env []string
	// HooksPath, when set, is used as core.hooksPath for receive-pack.
	HooksPath string
}

func (g *Git) serviceCmd(ctx context.Context, repoPath string, svc Service, r *http.Request, o RPCOptions, extra ...string) *exec.Cmd {
	args := []string{"-c", "uploadpack.allowFilter=true", "-c", "uploadpack.allowAnySHA1InWant=true"}
	if svc == ReceivePack && o.HooksPath != "" {
		args = append(args, "-c", "core.hooksPath="+o.HooksPath)
	}
	args = append(args, strings.TrimPrefix(string(svc), "git-"), "--stateless-rpc")
	args = append(args, extra...)
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, g.Binary, args...)
	cmd.Dir = repoPath
	cmd.Env = append(g.baseEnv(), o.Env...)
	if p := r.Header.Get("Git-Protocol"); p != "" && validGitProtocol(p) {
		cmd.Env = append(cmd.Env, "GIT_PROTOCOL="+p)
	}
	return cmd
}

// validGitProtocol accepts values like "version=2" (colon separated
// key=value pairs) and nothing else.
func validGitProtocol(p string) bool {
	if len(p) > 128 {
		return false
	}
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '=' || c == ':' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func noCache(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	h.Set("Pragma", "no-cache")
	h.Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
}

func pktLine(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// AdvertiseRefs handles GET /info/refs?service=...
func (g *Git) AdvertiseRefs(w http.ResponseWriter, r *http.Request, repoPath string, svc Service, o RPCOptions) error {
	cmd := g.serviceCmd(r.Context(), repoPath, svc, r, o, "--advertise-refs")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%s --advertise-refs: %w", svc, err)
	}
	noCache(w)
	w.Header().Set("Content-Type", "application/x-"+string(svc)+"-advertisement")
	if !strings.Contains(r.Header.Get("Git-Protocol"), "version=2") {
		_, _ = io.WriteString(w, pktLine("# service="+string(svc)+"\n")+"0000")
	}
	_, err = w.Write(out)
	return err
}

// ServeRPC handles POST /git-upload-pack or /git-receive-pack.
func (g *Git) ServeRPC(w http.ResponseWriter, r *http.Request, repoPath string, svc Service, o RPCOptions) error {
	if ct := r.Header.Get("Content-Type"); ct != "application/x-"+string(svc)+"-request" {
		http.Error(w, "unexpected content type", http.StatusUnsupportedMediaType)
		return nil
	}
	var body io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "bad gzip body", http.StatusBadRequest)
			return nil
		}
		defer gz.Close()
		body = gz
	}
	cmd := g.serviceCmd(r.Context(), repoPath, svc, r, o)
	cmd.Stdin = body
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{sb: &stderr, max: 32 << 10}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	noCache(w)
	w.Header().Set("Content-Type", "application/x-"+string(svc)+"-result")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s: %w: %s", svc, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type limitedWriter struct {
	sb  *strings.Builder
	max int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if room := l.max - l.sb.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		l.sb.Write(p)
	}
	return len(p), nil
}
