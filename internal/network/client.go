// Package network builds per-operation transports from the application's proxy
// settings. Explicit direct/manual settings never inherit environment proxies.
package network

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/netproxy"
	"image2ai/internal/domain"
)

func ValidateProxy(config domain.ProxyConfig) error {
	switch config.Mode {
	case "system", "direct":
		return nil
	case "manual":
		u, err := url.Parse(strings.TrimSpace(config.URL))
		if err != nil || u.Hostname() == "" || u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("代理地址无效，请输入不带账号密码和路径的 HTTP(S)/SOCKS 地址。")
		}
		switch u.Scheme {
		case "http", "https", "socks4", "socks5":
		default:
			return errors.New("代理仅支持 HTTP(S)、SOCKS4 和 SOCKS5。")
		}
		if p := u.Port(); p != "" {
			n, e := strconv.Atoi(p)
			if e != nil || n < 1 || n > 65535 {
				return errors.New("代理端口无效。")
			}
		}
		return nil
	default:
		return errors.New("代理模式无效。")
	}
}

// NewClient creates an isolated transport. CloseIdleConnections after an operation.
func NewClient(config domain.ProxyConfig, timeout time.Duration) (*http.Client, error) {
	if err := ValidateProxy(config); err != nil {
		return nil, err
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = 4
	t.ResponseHeaderTimeout = timeout
	var transport http.RoundTripper = t
	switch config.Mode {
	case "system":
		t = netproxy.Transport().Clone()
		transport = t
	case "direct":
		t.Proxy = nil
	case "manual":
		u, _ := url.Parse(strings.TrimSpace(config.URL))
		if u.Scheme == "socks4" {
			t.Proxy = nil
			direct := t.Clone()
			dial := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialSOCKS4(ctx, dial, u, address)
			}
			transport = &routedTransport{proxy: t, direct: direct, rules: config.Bypass}
		} else {
			t.Proxy = func(req *http.Request) (*url.URL, error) {
				if bypassesURL(req.URL, config.Bypass) {
					return nil, nil
				}
				return u, nil
			}
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("图片重定向次数过多。")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("图片重定向地址无效。")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Proxy-Authorization")
		return nil
	}}, nil
}

type routedTransport struct {
	proxy, direct *http.Transport
	rules         string
}

func (t *routedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if bypassesURL(request.URL, t.rules) {
		return t.direct.RoundTrip(request)
	}
	return t.proxy.RoundTrip(request)
}
func (t *routedTransport) CloseIdleConnections() {
	t.proxy.CloseIdleConnections()
	t.direct.CloseIdleConnections()
}

func bypassesURL(target *url.URL, rules string) bool {
	hostname := target.Hostname()
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	applicable := []string{}
	for _, raw := range strings.FieldsFunc(rules, func(r rune) bool { return r == ',' || r == ';' }) {
		rule := strings.TrimSpace(raw)
		if scheme, rest, ok := strings.Cut(rule, "://"); ok {
			if !strings.EqualFold(scheme, target.Scheme) {
				continue
			}
			rule = rest
		}
		if host, p, err := net.SplitHostPort(rule); err == nil {
			if p != port {
				continue
			}
			rule = host
		}
		applicable = append(applicable, rule)
	}
	return Bypasses(hostname, strings.Join(applicable, ","))
}

// Bypasses supports Chromium-style local names, glob hosts, CIDRs and comma/semicolon lists.
func Bypasses(host, rules string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	ip := net.ParseIP(host)
	// Chromium sends loopback requests directly unless explicitly opted out.
	loopback := host == "localhost" || strings.HasSuffix(host, ".localhost") || ip != nil && ip.IsLoopback()
	if loopback && !strings.Contains(rules, "<-loopback>") {
		return true
	}
	for _, raw := range strings.FieldsFunc(strings.ToLower(rules), func(r rune) bool { return r == ',' || r == ';' }) {
		rule := strings.TrimSpace(raw)
		if rule == "<local>" && !strings.Contains(host, ".") && !strings.Contains(host, ":") {
			return true
		}
		if rule == "" || rule == "<-loopback>" {
			continue
		}
		if _, network, err := net.ParseCIDR(rule); err == nil && ip != nil && network.Contains(ip) {
			return true
		}
		if strings.Contains(rule, "://") {
			if u, err := url.Parse(rule); err == nil {
				rule = u.Hostname()
			}
		}
		if h, _, err := net.SplitHostPort(rule); err == nil {
			rule = h
		}
		if strings.HasPrefix(rule, ".") && (strings.HasSuffix(host, rule) || host == strings.TrimPrefix(rule, ".")) {
			return true
		}
		if match, _ := path.Match(rule, host); match {
			return true
		}
	}
	return false
}

func dialSOCKS4(ctx context.Context, dial *net.Dialer, proxy *url.URL, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("SOCKS4 目标地址无效。")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return nil, errors.New("SOCKS4 目标端口无效。")
	}
	// Chromium SOCKS4 resolves names locally and supports IPv4 only.
	ip := net.ParseIP(host).To4()
	if ip == nil {
		addresses, e := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if e != nil || len(addresses) == 0 {
			return nil, errors.New("SOCKS4 无法解析 IPv4 目标地址。")
		}
		ip = addresses[0].To4()
	}
	if ip == nil {
		return nil, errors.New("SOCKS4 不支持 IPv6 目标。")
	}
	endpoint := proxy.Host
	if proxy.Port() == "" {
		endpoint = net.JoinHostPort(proxy.Hostname(), "1080")
	}
	conn, err := dial.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return nil, errors.New("无法连接 SOCKS4 代理。")
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	request := []byte{4, 1, 0, 0, ip[0], ip[1], ip[2], ip[3], 0}
	binary.BigEndian.PutUint16(request[2:4], uint16(p))
	if _, err = conn.Write(request); err != nil {
		return nil, errors.New("SOCKS4 握手失败。")
	}
	response := make([]byte, 8)
	if _, err = io.ReadFull(conn, response); err != nil {
		return nil, errors.New("SOCKS4 握手失败。")
	}
	if response[0] != 0 || response[1] != 90 {
		return nil, fmt.Errorf("SOCKS4 代理拒绝连接（%d）。", response[1])
	}
	conn.SetDeadline(time.Time{})
	success = true
	return conn, nil
}
