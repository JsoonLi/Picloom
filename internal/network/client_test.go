package network

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"image2ai/internal/domain"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyValidationAndBypass(t *testing.T) {
	for _, raw := range []string{"file:///secret", "ftp://host:21", "http://local-test:local-test@host:123", "127.0.0.1:7890", "http://host:123/path", "http://host:0", "http://host:65536"} {
		if ValidateProxy(domain.ProxyConfig{Mode: "manual", URL: raw}) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:7890", "https://proxy.test:443", "socks4://127.0.0.1:1080", "socks5://127.0.0.1:1080"} {
		if err := ValidateProxy(domain.ProxyConfig{Mode: "manual", URL: raw}); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"direct", "system"} {
		if ValidateProxy(domain.ProxyConfig{Mode: mode}) != nil {
			t.Fatal(mode)
		}
	}
	if ValidateProxy(domain.ProxyConfig{Mode: "unknown"}) == nil {
		t.Fatal("accepted unknown mode")
	}
	for _, host := range []string{"localhost", "127.0.0.2", "::1", "printer", "api.example.test", "10.2.3.4"} {
		if !Bypasses(host, "<local>;*.example.test,10.0.0.0/8") {
			t.Fatal(host)
		}
	}
	if Bypasses("other.test", "<local>,*.example.test") {
		t.Fatal("bypassed unrelated host")
	}
	if Bypasses("127.0.0.1", "<-loopback>") {
		t.Fatal("ignored loopback opt-out")
	}
}
func TestHTTPProxyAndDirectOverrideEnvironment(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "example.invalid" {
			t.Errorf("target %q", r.URL.Host)
		}
		io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	client, err := NewClient(domain.ProxyConfig{Mode: "manual", URL: proxy.URL}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	r, err := client.Get("http://example.invalid/test")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(data) != "proxied" {
		t.Fatal(string(data))
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct") }))
	defer local.Close()
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:1")
	direct, err := NewClient(domain.ProxyConfig{Mode: "direct"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer direct.CloseIdleConnections()
	r, err = direct.Get(local.URL)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
}

func TestHTTPSProxyAndSystemLoopback(t *testing.T) {
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secure proxy") }))
	defer proxy.Close()
	client, err := NewClient(domain.ProxyConfig{Mode: "manual", URL: proxy.URL}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	response, err := client.Get("http://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "secure proxy" {
		t.Fatal(string(data))
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "system direct loopback") }))
	defer local.Close()
	system, err := NewClient(domain.ProxyConfig{Mode: "system"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer system.CloseIdleConnections()
	response, err = system.Get(local.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestBypassSchemeAndPortAreNotDiscarded(t *testing.T) {
	for _, test := range []struct {
		target, rules string
		want          bool
	}{
		{"https://api.example.test/", "https://*.example.test:443", true},
		{"http://api.example.test/", "https://*.example.test:443", false},
		{"https://api.example.test:8443/", "*.example.test:443", false},
		{"http://api.example.test:8080/", "*.example.test:8080", true},
	} {
		target, _ := url.Parse(test.target)
		if got := bypassesURL(target, test.rules); got != test.want {
			t.Fatalf("%s %s: %v", test.target, test.rules, got)
		}
	}
}
func TestSOCKS4HandshakeAndRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		header := make([]byte, 9)
		if _, e = io.ReadFull(conn, header); e != nil {
			done <- e
			return
		}
		if header[0] != 4 || header[1] != 1 || header[2] != 31 || header[3] != 144 || header[4] != 192 || header[7] != 1 || header[8] != 0 {
			done <- io.ErrUnexpectedEOF
			return
		}
		conn.Write([]byte{0, 90, 0, 0, 0, 0, 0, 0})
		reader := bufio.NewReader(conn)
		request, e := http.ReadRequest(reader)
		if e != nil {
			done <- e
			return
		}
		request.Body.Close()
		_, e = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nConnection: close\r\n\r\nsocks4!")
		done <- e
	}()
	client, err := NewClient(domain.ProxyConfig{Mode: "manual", URL: "socks4://" + listener.Addr().String()}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://192.0.2.1:8080/image")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "socks4!" {
		t.Fatal(string(data))
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestSOCKS5HandshakeAndRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		greeting := make([]byte, 3)
		if _, e = io.ReadFull(conn, greeting); e != nil {
			done <- e
			return
		}
		conn.Write([]byte{5, 0})
		header := make([]byte, 4)
		if _, e = io.ReadFull(conn, header); e != nil {
			done <- e
			return
		}
		length := 0
		switch header[3] {
		case 1:
			length = 4
		case 3:
			b := make([]byte, 1)
			io.ReadFull(conn, b)
			length = int(b[0])
		case 4:
			length = 16
		}
		if _, e = io.CopyN(io.Discard, conn, int64(length+2)); e != nil {
			done <- e
			return
		}
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
		req, e := http.ReadRequest(bufio.NewReader(conn))
		if e != nil {
			done <- e
			return
		}
		req.Body.Close()
		_, e = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nConnection: close\r\n\r\nsocks5!")
		done <- e
	}()
	client, err := NewClient(domain.ProxyConfig{Mode: "manual", URL: "socks5://" + listener.Addr().String()}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	r, err := client.Get("http://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(data) != "socks5!" {
		t.Fatal(string(data))
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestSOCKS4CancellationAndRejection(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "reject"}[reject], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			received := make(chan struct{})
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				header := make([]byte, 9)
				io.ReadFull(conn, header)
				close(received)
				if reject {
					conn.Write([]byte{0, 91, 0, 0, 0, 0, 0, 0})
					return
				}
				io.Copy(io.Discard, conn)
			}()
			client, err := NewClient(domain.ProxyConfig{Mode: "manual", URL: "socks4://" + listener.Addr().String()}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !reject {
				go func() { <-received; cancel() }()
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", "http://192.0.2.1/", nil)
			_, err = client.Do(req)
			if err == nil {
				t.Fatal("expected failure")
			}
			if reject && !strings.Contains(err.Error(), "91") {
				t.Fatal(err)
			}
		})
	}
}
