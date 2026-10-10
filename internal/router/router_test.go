package router

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	for _, s := range []string{"a..b", "-bad", "foo-", "x\ny", "a;ls", strings.Repeat("a", 64)} {
		if _, err := Name(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if got, _ := Name("Feed.localhost"); got != "feed" {
		t.Fatal(got)
	}
	if got, _ := Name("Map.App.localhost"); got != "map.app" {
		t.Fatal(got)
	}
	if err := Update(dir, func(c Config) error { c["feed"] = 4173; c["docs"] = 3000; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(c Config) error { c["feed"] = 5173; delete(c, "docs"); return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ := Read(dir)
	if len(c) != 1 || c["feed"] != 5173 {
		t.Fatal(c)
	}
}
func TestProxyRoutesLiveAndPreservesOrigin(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s|%s|%s|%s", r.Host, r.URL.RequestURI(), r.Header.Get("Origin"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-For"))
	}))
	defer upstream.Close()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(p)
	dir := t.TempDir()
	_ = Update(dir, func(c Config) error { c["feed"] = port; return nil })
	handler := Handler(dir, false)
	req := httptest.NewRequest("GET", "http://feed.localhost/api?q=hello", nil)
	req.Header.Set("Origin", "https://feed.localhost")
	req.Header.Set("X-Forwarded-For", "evil")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "feed.localhost|/api?q=hello|https://feed.localhost|feed.localhost|") || strings.Contains(w.Body.String(), "evil") {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = Update(dir, func(c Config) error { delete(c, "feed"); return nil })
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestDevelopmentServiceOverridesOnlyWhileRegistered(t *testing.T) {
	persistent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "persistent") }))
	defer persistent.Close()
	development := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "development") }))
	defer development.Close()
	portOf := func(server *httptest.Server) int {
		_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		value, _ := strconv.Atoi(port)
		return value
	}
	dir := t.TempDir()
	if err := Update(dir, func(config Config) error { config["app.project"] = portOf(persistent); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDevServices(dir, func(services map[string]DevService) error {
		services["app.project"] = DevService{Port: portOf(development), Owner: "development-owner"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://app.project.localhost/", nil)
	response := httptest.NewRecorder()
	Handler(dir, false).ServeHTTP(response, request)
	if response.Body.String() != "development" {
		t.Fatalf("development route did not take precedence: %q", response.Body.String())
	}
	if err := UpdateDevServices(dir, func(services map[string]DevService) error {
		delete(services, "app.project")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	Handler(dir, false).ServeHTTP(response, request)
	if response.Body.String() != "persistent" {
		t.Fatalf("persistent route was not restored: %q", response.Body.String())
	}
}

func TestRedirectUnknownAndUnavailable(t *testing.T) {
	dir := t.TempDir()
	_ = Update(dir, func(c Config) error { c["feed"] = 1; return nil })
	req := httptest.NewRequest("GET", "http://feed.localhost/a?b=c", nil)
	w := httptest.NewRecorder()
	Handler(dir, true).ServeHTTP(w, req)
	if w.Code != 308 || w.Header().Get("Location") != "https://feed.localhost/a?b=c" {
		t.Fatal(w)
	}
	w = httptest.NewRecorder()
	Handler(dir, false).ServeHTTP(w, req)
	if w.Code != 502 {
		t.Fatal(w.Code)
	}
	req.Host = "evil.test"
	w = httptest.NewRecorder()
	Handler(dir, true).ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	req.Host = "feed.localhost"
	req.TLS = &tls.ConnectionState{ServerName: "other.localhost"}
	w = httptest.NewRecorder()
	Handler(dir, false).ServeHTTP(w, req)
	if w.Code != 421 {
		t.Fatal(w.Code)
	}
}
func TestAuthorityTLSAndUnregisteredNames(t *testing.T) {
	dir := t.TempDir()
	_ = Update(dir, func(c Config) error { c["feed"] = 4173; return nil })
	_ = UpdateDevServices(dir, func(services map[string]DevService) error {
		services["app.project"] = DevService{Port: 4174, Owner: "development-owner"}
		return nil
	})
	a, err := LoadAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadAuthority(dir)
	if err != nil || a.Fingerprint() != second.Fingerprint() {
		t.Fatal("CA did not persist", err)
	}
	info, _ := os.Stat(filepath.Join(dir, "root.key"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	for _, name := range []string{"evil.com", "localhost"} {
		if _, err := a.GetCertificate(&tls.ClientHelloInfo{ServerName: name}); err == nil {
			t.Fatal("issued unknown certificate")
		}
	}
	if _, err := a.GetCertificate(&tls.ClientHelloInfo{ServerName: "unknown.localhost"}); err != nil {
		t.Fatal(err)
	}
	cert, err := a.GetCertificate(&tls.ClientHelloInfo{ServerName: "feed.localhost"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(a.PEM)
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "feed.localhost"}); err != nil {
		t.Fatal(err)
	}
	nested, err := a.GetCertificate(&tls.ClientHelloInfo{ServerName: "app.project.localhost"})
	if err != nil {
		t.Fatal(err)
	}
	nestedLeaf, _ := x509.ParseCertificate(nested.Certificate[0])
	if _, err = nestedLeaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "app.project.localhost"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "TLS works") }))
	server.TLS = &tls.Config{GetCertificate: a.GetCertificate}
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "feed.localhost"}}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	res, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "TLS works" {
		t.Fatal(string(body))
	}
}
func TestWebSocketUpgradeTunnel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		line, _ := rw.ReadString('\n')
		fmt.Fprint(rw, line)
		rw.Flush()
	}))
	defer upstream.Close()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(p)
	dir := t.TempDir()
	_ = Update(dir, func(c Config) error { c["feed"] = port; return nil })
	proxy := httptest.NewServer(Handler(dir, false))
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "GET /socket HTTP/1.1\r\nHost: feed.localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	reader := bufio.NewReader(conn)
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "101") {
		t.Fatal(line)
	}
	for {
		line, _ = reader.ReadString('\n')
		if line == "\r\n" {
			break
		}
	}
	fmt.Fprint(conn, "hello through upgrade\n")
	line, err = reader.ReadString('\n')
	if err != nil || line != "hello through upgrade\n" {
		t.Fatal(line, err)
	}
}
