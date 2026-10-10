package router

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const availabilityPath = "/_loca/wait"

const waitingPage = `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta name="robots" content="noindex">
	<title>Waiting for local service</title>
	<style>
		:root { color-scheme: light dark; font: 16px/1.5 system-ui, sans-serif; }
		body { min-height: 100vh; margin: 0; display: grid; place-items: center; background: #f4f5f7; color: #252932; }
		main { max-width: 32rem; padding: 2rem; }
		h1 { font-size: 1.4rem; }
		p { color: #626977; }
		@media (prefers-color-scheme: dark) { body { background: #17191d; color: #f1f2f4; } p { color: #a9afba; } }
	</style>
</head>
<body>
	<main>
		<h1>Waiting for <span id="host"></span></h1>
		<p>This page will reload when the local service is available.</p>
	</main>
	<script>
		document.getElementById("host").textContent = location.host;
		async function waitForService() {
			try {
				const response = await fetch("/_loca/wait", { cache: "no-store" });
				if (response.status === 204) {
					location.reload();
					return;
				}
			} catch (_) {}
			setTimeout(waitForService, 1000);
		}
		waitForService();
	</script>
</body>
</html>`

func HostName(host string) (string, error) {
	if strings.Contains(host, ":") {
		h, _, err := net.SplitHostPort(host)
		if err != nil {
			return "", err
		}
		host = h
	}
	if !strings.HasSuffix(host, ".localhost") {
		return "", fmt.Errorf("not a .localhost host")
	}
	name, err := Name(host)
	if err != nil {
		return "", err
	}
	return name, nil
}
func Handler(dir string, redirect bool) http.Handler {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 100}
	control := ControlHandler(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if strings.EqualFold(host, "localhost") {
			control.ServeHTTP(w, r)
			return
		}
		name, err := HostName(r.Host)
		if err != nil {
			http.Error(w, "Unknown local service", http.StatusNotFound)
			return
		}
		if r.URL.Path == availabilityPath && r.Method == http.MethodGet {
			if serviceAvailable(dir, name) {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		config, err := Read(dir)
		if err != nil {
			http.Error(w, "Loca could not read its mappings", http.StatusServiceUnavailable)
			return
		}
		devServices, err := ReadDevServices(dir)
		if err != nil {
			http.Error(w, "Loca could not read development services", http.StatusServiceUnavailable)
			return
		}
		service, isDevService := devServices[name]
		port, ok := config[name]
		if isDevService {
			port, ok = service.Port, true
		}
		if !ok {
			if wantsHTML(r) {
				serveWaitingPage(w, r, http.StatusNotFound)
				return
			}
			http.Error(w, "Unknown local service. Register it with loca set.", http.StatusNotFound)
			return
		}
		if r.TLS != nil && r.TLS.ServerName != name+".localhost" {
			http.Error(w, "Host and TLS name must match", http.StatusMisdirectedRequest)
			return
		}
		if redirect {
			u := *r.URL
			u.Scheme = "https"
			u.Host = name + ".localhost"
			http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
			return
		}
		target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
		proxy := &httputil.ReverseProxy{Transport: transport, FlushInterval: -1, Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.Host = p.In.Host
			p.SetXForwarded()
			if r.TLS != nil {
				p.Out.Header.Set("X-Forwarded-Proto", "https")
			}
		}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
			log.Printf("upstream %s:%d: %v", name, port, e)
			if wantsHTML(r) {
				serveWaitingPage(w, r, http.StatusBadGateway)
				return
			}
			http.Error(w, fmt.Sprintf("%s is registered but port %d is unavailable. Start that service and reload.", name, port), http.StatusBadGateway)
		}}
		proxy.ServeHTTP(w, r)
	})
}

func wantsHTML(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func serveWaitingPage(w http.ResponseWriter, r *http.Request, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(waitingPage))
}

func serviceAvailable(dir, name string) bool {
	config, err := Read(dir)
	if err != nil {
		return false
	}
	devServices, err := ReadDevServices(dir)
	if err != nil {
		return false
	}
	port, ok := config[name]
	if service, isDevService := devServices[name]; isDevService {
		port, ok = service.Port, true
	}
	if !ok {
		return false
	}
	conn, err := (&net.Dialer{Timeout: 300 * time.Millisecond}).Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
