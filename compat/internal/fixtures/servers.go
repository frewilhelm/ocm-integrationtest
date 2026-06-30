package fixtures

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/cli"
)

// listenAddr picks the bind address by CLI mode. Docker mode needs
// 0.0.0.0 so the listener is reachable from inside the container via
// host.docker.internal; bin mode runs everything on the host and
// 127.0.0.1 sidesteps macOS's firewall prompt for 0.0.0.0 binds.
// The reported URL keeps 127.0.0.1 in both cases; URLForContainer
// substitutes host.docker.internal in docker mode.
func listenAddr() string {
	if cli.V1.Mode == cli.ModeDocker {
		return "0.0.0.0:0"
	}
	return "127.0.0.1:0"
}

// StartHTTPFileServer serves the given path -> bytes map over a fresh
// listener; see listenAddr for the bind choice and URLForContainer for
// the host-to-container URL rewrite.
func StartHTTPFileServer(files map[string][]byte) (string, Cleanup, error) {
	mux := http.NewServeMux()
	for p, data := range files {
		p, data := p, data
		mux.HandleFunc("/"+p, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(data)
		})
	}
	listener, err := net.Listen("tcp", listenAddr())
	if err != nil {
		return "", nil, fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{Handler: mux}
	go serveHTTP(srv, listener, "httpFileServer")
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownHTTP(srv, ctx, "httpFileServer")
	}
	addr := listener.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d", addr.Port), cleanup, nil
}

// StartHermeticMavenRepo serves a minimal Maven 2 layout for one
// artifact (POM + JAR + checksums + maven-metadata.xml + HTML index).
//
// The HTML index at <gav>/ is the non-obvious bit: v1's Maven access
// (gavOnlineFiles) GETs the GAV directory and parses <a href="..."> to
// discover artifact files. The jar payload is deterministic bytes;
// neither leg unpacks it.
func StartHermeticMavenRepo(group, artifact, version string, jarBytes []byte) (string, Cleanup, error) {
	groupPath := strings.ReplaceAll(group, ".", "/")
	pom := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>%s</groupId>
  <artifactId>%s</artifactId>
  <version>%s</version>
  <packaging>jar</packaging>
</project>
`, group, artifact, version)
	pomBytes := []byte(pom)
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>%s</groupId>
  <artifactId>%s</artifactId>
  <versioning>
    <latest>%s</latest>
    <release>%s</release>
    <versions><version>%s</version></versions>
    <lastUpdated>20240101000000</lastUpdated>
  </versioning>
</metadata>
`, group, artifact, version, version, version)

	sha1Hex := func(b []byte) string { s := sha1.Sum(b); return hex.EncodeToString(s[:]) }
	md5Hex := func(b []byte) string { s := md5.Sum(b); return hex.EncodeToString(s[:]) }

	base := fmt.Sprintf("/%s/%s", groupPath, artifact)
	verBase := fmt.Sprintf("%s/%s", base, version)
	jarFile := fmt.Sprintf("%s-%s.jar", artifact, version)
	pomFile := fmt.Sprintf("%s-%s.pom", artifact, version)

	files := map[string][]byte{
		base + "/maven-metadata.xml":      []byte(metadata),
		base + "/maven-metadata.xml.sha1": []byte(sha1Hex([]byte(metadata))),
		base + "/maven-metadata.xml.md5":  []byte(md5Hex([]byte(metadata))),
		verBase + "/" + pomFile:           pomBytes,
		verBase + "/" + pomFile + ".sha1": []byte(sha1Hex(pomBytes)),
		verBase + "/" + pomFile + ".md5":  []byte(md5Hex(pomBytes)),
		verBase + "/" + jarFile:           jarBytes,
		verBase + "/" + jarFile + ".sha1": []byte(sha1Hex(jarBytes)),
		verBase + "/" + jarFile + ".md5":  []byte(md5Hex(jarBytes)),
	}

	listing := func(names []string) []byte {
		var b strings.Builder
		b.WriteString("<html><body>\n")
		for _, n := range names {
			fmt.Fprintf(&b, `<a href="%s">%s</a>`+"\n", n, n)
		}
		b.WriteString("</body></html>\n")
		return []byte(b.String())
	}
	verListing := listing([]string{
		pomFile, pomFile + ".sha1", pomFile + ".md5",
		jarFile, jarFile + ".sha1", jarFile + ".md5",
	})

	mux := http.NewServeMux()
	for p, data := range files {
		p, data := p, data
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(data)
		})
	}
	// Trailing-slash registration so the per-file handlers still win
	// for /.../foo.jar.
	mux.HandleFunc(verBase+"/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != verBase+"/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(verListing)
	})
	listener, err := net.Listen("tcp", listenAddr())
	if err != nil {
		return "", nil, fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{Handler: mux}
	go serveHTTP(srv, listener, "hermeticMavenRepo")
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownHTTP(srv, ctx, "hermeticMavenRepo")
	}
	addr := listener.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d", addr.Port), cleanup, nil
}

// serveHTTP runs srv.Serve and forwards anything other than the
// expected clean-shutdown signal to GinkgoWriter. Without this trace
// the case that backs the listener fails as a generic "construct
// exited 1" with no hint that the fixture was the cause.
func serveHTTP(srv *http.Server, listener net.Listener, label string) {
	ginkgo.GinkgoHelper()
	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(ginkgo.GinkgoWriter, "fixtures: %s serve error: %v\n", label, err)
	}
}

// shutdownHTTP surfaces shutdown errors (typically
// context.DeadlineExceeded on a wedged handler) so a leaked listener
// shows up in the test log instead of silently outliving the case.
func shutdownHTTP(srv *http.Server, ctx context.Context, label string) {
	ginkgo.GinkgoHelper()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Fprintf(ginkgo.GinkgoWriter, "fixtures: %s shutdown error: %v\n", label, err)
	}
}
