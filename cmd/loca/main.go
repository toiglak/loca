package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/toiglak/loca/internal/router"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const label = "dev.iglak.loca"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "loca:", err)
		os.Exit(1)
	}
}
func home() string { h, _ := os.UserHomeDir(); return h }
func appBinary() string {
	return filepath.Join(home(), "Applications", "Loca.app", "Contents", "MacOS", "Loca")
}
func plistPath() string { return filepath.Join("/Library", "LaunchDaemons", label+".plist") }
func target() string    { return "system/" + label }
func command(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}
func quiet(name string, args ...string) error { return exec.Command(name, args...).Run() }
func output(name string, args ...string) string {
	b, _ := exec.Command(name, args...).CombinedOutput()
	return string(b)
}
func esc(s string) string   { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
func shell(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func sorted(c router.Config) []string {
	names := make([]string, 0, len(c))
	for n := range c {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func reachable(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 600*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
func run(args []string) error {
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	dir := router.Dir()
	switch cmd {
	case "set":
		if len(args) != 2 {
			return fmt.Errorf("usage: loca set <name> <port>")
		}
		name, err := router.Name(args[0])
		if err != nil {
			return err
		}
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 || port == 80 || port == 443 {
			return fmt.Errorf("use an application port from 1–65535 other than 80/443 (reserved for loca)")
		}
		err = router.Update(dir, func(c router.Config) error {
			for n, p := range c {
				if n != name && p == port {
					fmt.Printf("Note: %s also uses port %d; both names will route there.\n", n, port)
				}
			}
			c[name] = port
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Printf("https://%s.localhost → http://127.0.0.1:%d (live while loca is running)\n", name, port)
		return nil
	case "remove", "rm":
		if len(args) != 1 {
			return fmt.Errorf("usage: loca remove <name>")
		}
		name, err := router.Name(args[0])
		if err != nil {
			return err
		}
		err = router.Update(dir, func(c router.Config) error {
			if _, ok := c[name]; !ok {
				return fmt.Errorf("no service named %s", name)
			}
			delete(c, name)
			return nil
		})
		if err == nil {
			fmt.Println("Removed", name)
		}
		return err
	case "list":
		c, err := router.Read(dir)
		if err != nil {
			return err
		}
		fmt.Printf("%-20s %-40s %-8s %s\n", "SERVICE", "ADDRESS", "PORT", "REACHABLE")
		for _, name := range sorted(c) {
			state := "no"
			if reachable(c[name]) {
				state = "yes"
			}
			fmt.Printf("%-20s %-40s %-8d %s\n", name, "https://"+name+".localhost", c[name], state)
		}
		return nil
	case "run":
		return serve(dir)
	case "trust":
		return trust(dir)
	case "install", "start":
		return install(dir)
	case "stop", "uninstall":
		script := "/bin/launchctl bootout " + shell(target())
		if cmd == "uninstall" {
			script += "; /bin/rm -f " + shell(plistPath())
		}
		if err := elevated(script); err != nil {
			return err
		}
		fmt.Println("Loca stopped. Mappings and local CA retained.")
		return nil
	case "status":
		text := output("launchctl", "print", target())
		if !strings.Contains(text, "state =") {
			fmt.Println("Loca is not running. Use loca start.")
			return nil
		}
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "state =") || strings.Contains(line, "pid =") || strings.Contains(line, "last exit code") {
				fmt.Println(strings.TrimSpace(line))
			}
		}
		return nil
	case "doctor":
		fmt.Println("Data:", dir)
		fmt.Println("Executable:", appBinary())
		fmt.Println("HTTP port 80:", reachable(80))
		fmt.Println("HTTPS port 443:", reachable(443))
		a, err := router.LoadAuthority(dir)
		if err != nil {
			return err
		}
		fmt.Println("Local CA SHA-256:", a.Fingerprint())
		if trusted(dir) {
			fmt.Println("Certificate trust: installed")
		} else {
			fmt.Println("Certificate trust: run loca trust")
		}
		return run([]string{"status"})
	case "help", "--help", "-h":
		fmt.Println("loca set <name> <port>\nloca remove <name>\nloca list\nloca open <name> (or loca <name>)\nloca start|stop|status|install|uninstall\nloca trust   # trust this machine’s Loca CA (macOS authorization)\nloca doctor\nloca run")
		return nil
	default:
		nameArg := cmd
		if cmd == "open" {
			if len(args) != 1 {
				return fmt.Errorf("usage: loca open <name>")
			}
			nameArg = args[0]
		}
		name, err := router.Name(nameArg)
		if err != nil {
			return err
		}
		c, err := router.Read(dir)
		if err != nil {
			return err
		}
		if _, ok := c[name]; !ok {
			return fmt.Errorf("unknown service: %s", name)
		}
		return command("open", "https://"+name+".localhost")
	}
}
func trusted(dir string) bool {
	return quiet("/usr/bin/security", "verify-cert", "-c", filepath.Join(dir, "root.crt"), "-p", "basic") == nil
}
func trust(dir string) error {
	authority, err := router.LoadAuthority(dir)
	if err != nil {
		return err
	}
	if trusted(dir) {
		fmt.Println("Loca’s local CA is already trusted.")
		return nil
	}
	fmt.Println("Trusting Loca’s machine-specific local CA. SHA-256:", authority.Fingerprint())
	fmt.Println("macOS may ask to trust the local CA in your login keychain. The private key stays on this Mac.")
	if err = command("/usr/bin/security", "add-trusted-cert", "-r", "trustRoot", "-k", filepath.Join(home(), "Library/Keychains/login.keychain-db"), filepath.Join(dir, "root.crt")); err != nil {
		return fmt.Errorf("certificate trust was not authorized: %w; retry loca trust from your logged-in session", err)
	}
	if !trusted(dir) {
		return fmt.Errorf("certificate trust could not be verified; run loca doctor")
	}
	return nil
}
func elevated(script string) error {
	return command("/usr/bin/osascript", "-e", "on run argv", "-e", "do shell script (item 1 of argv) with administrator privileges", "-e", "end run", script)
}
func installationPlist(dir string) string {
	user := os.Getenv("USER")
	sockets := ""
	for _, entry := range []struct{ name, host, family, port string }{{"HTTPv4", "127.0.0.1", "IPv4", "80"}, {"HTTPSv4", "127.0.0.1", "IPv4", "443"}, {"HTTPv6", "::1", "IPv6", "80"}, {"HTTPSv6", "::1", "IPv6", "443"}} {
		sockets += `<key>` + entry.name + `</key><dict><key>SockNodeName</key><string>` + entry.host + `</string><key>SockServiceName</key><string>` + entry.port + `</string><key>SockFamily</key><string>` + entry.family + `</string><key>SockType</key><string>stream</string><key>SockPassive</key><true/></dict>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + label + `</string><key>ProgramArguments</key><array><string>` + esc(appBinary()) + `</string><string>run</string></array><key>UserName</key><string>` + esc(user) + `</string><key>AssociatedBundleIdentifiers</key><array><string>dev.iglak.Loca</string></array><key>EnvironmentVariables</key><dict><key>HOME</key><string>` + esc(home()) + `</string><key>LOCA_DATA_DIR</key><string>` + esc(dir) + `</string><key>LOCA_SOCKET_ACTIVATION</key><string>1</string></dict><key>Sockets</key><dict>` + sockets + `</dict><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>10</integer><key>StandardOutPath</key><string>` + esc(filepath.Join(dir, "daemon.log")) + `</string><key>StandardErrorPath</key><string>` + esc(filepath.Join(dir, "daemon.error.log")) + `</string></dict></plist>`
}
func install(dir string) error {
	if _, err := os.Stat(appBinary()); err != nil {
		return fmt.Errorf("build the named Loca app first: ./scripts/install.sh")
	}
	a, err := router.LoadAuthority(dir)
	if err != nil {
		return err
	}
	loaded := quiet("launchctl", "print", target()) == nil
	if !loaded {
		for _, port := range []int{80, 443} {
			if reachable(port) {
				return fmt.Errorf("port %d is already in use; stop the conflicting router before starting loca", port)
			}
		}
	}
	staging := filepath.Join(dir, "dev.iglak.loca.plist")
	if err = router.AtomicWrite(staging, []byte(installationPlist(dir)), 0644); err != nil {
		return err
	}
	fmt.Println("Installing Loca.app as a named system service running as your user.")
	fmt.Println("macOS opens loopback ports 80/443; Loca receives the sockets without root privileges.")
	fmt.Println("Local CA SHA-256:", a.Fingerprint())
	script := ""
	if loaded {
		script += "/bin/launchctl bootout " + shell(target()) + "; "
	}
	script += "/usr/bin/install -o root -g wheel -m 644 " + shell(staging) + " " + shell(plistPath()) + " && /bin/launchctl bootstrap system " + shell(plistPath())
	if os.Getenv("LOCA_PREPARE_ONLY") == "1" {
		fmt.Println("Prepared authorization script:\n" + script)
		return nil
	}
	if err = elevated(script); err != nil {
		return fmt.Errorf("macOS authorization or service installation failed: %w", err)
	}
	for i := 0; i < 50; i++ {
		if reachable(80) && reachable(443) {
			fmt.Println("Loca is running. HTTPS routing persists across restarts.")
			return trust(dir)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Loca did not start; see %s", filepath.Join(dir, "daemon.error.log"))
}
func serve(dir string) error {
	a, err := router.LoadAuthority(dir)
	if err != nil {
		return err
	}
	if _, err = router.EnsureControlToken(dir); err != nil {
		return fmt.Errorf("could not initialize Loca control API: %w", err)
	}
	servers := []*http.Server{}
	listeners := []net.Listener{}
	// Bind only loopback; never expose personal apps to the LAN.
	for _, host := range []string{"127.0.0.1", "::1"} {
		for _, port := range []int{80, 443} {
			addr := net.JoinHostPort(host, strconv.Itoa(port))
			var listener net.Listener
			var e error
			if os.Getenv("LOCA_SOCKET_ACTIVATION") == "1" {
				name := "HTTP"
				if port == 443 {
					name = "HTTPS"
				}
				if host == "::1" {
					name += "v6"
				} else {
					name += "v4"
				}
				listener, e = router.ActivatedListener(name)
			} else {
				listener, e = net.Listen("tcp", addr)
			}
			if e != nil {
				for _, l := range listeners {
					l.Close()
				}
				return fmt.Errorf("cannot bind %s: %w (another router may be running)", addr, e)
			}
			listeners = append(listeners, listener)
			server := &http.Server{Handler: router.Handler(dir, port == 80), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
			if port == 443 {
				listener = tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: a.GetCertificate})
			}
			servers = append(servers, server)
			go func(s *http.Server, l net.Listener) {
				if e := s.Serve(l); e != nil && !errors.Is(e, http.ErrServerClosed) {
					log.Printf("server error: %v", e)
				}
			}(server, listener)
		}
	}
	log.Println("Loca: HTTP and HTTPS on loopback, mappings reload live")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	<-signals
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		_ = server.Shutdown(ctx)
	}
	return nil
}
