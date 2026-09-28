package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRenderProxyTemplateCreatesFreshSession(t *testing.T) {
	template := "http://node.{uuid}:secret@127.0.0.1:9200"
	first, err := renderProxyTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderProxyTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("separate renders must use different Resin sessions")
	}
	for _, rendered := range []string{first, second} {
		if strings.Contains(rendered, "{uuid}") {
			t.Fatalf("placeholder was not rendered: %s", maskProxyURL(rendered))
		}
		u, err := url.Parse(rendered)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(u.User.Username(), "node.") || len(u.User.Username()) != len("node.")+36 {
			t.Fatalf("unexpected Resin username %q", u.User.Username())
		}
		if password, _ := u.User.Password(); password != "secret" {
			t.Fatal("proxy password changed while rendering")
		}
	}
}

func TestRenderProxyTemplateSupportsUUIDHex(t *testing.T) {
	rendered, err := renderProxyTemplate("http://node.{uuid_hex}:secret@127.0.0.1:9200")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(rendered)
	if len(u.User.Username()) != len("node.")+32 {
		t.Fatalf("unexpected hex username %q", u.User.Username())
	}
}

func TestValidateResinProxyTemplates(t *testing.T) {
	valid := []string{"http://node.{uuid}:secret@127.0.0.1:9200"}
	if err := validateResinProxyTemplates(resinProxyStrategy, valid); err != nil {
		t.Fatalf("valid Resin template rejected: %v", err)
	}
	for _, proxies := range [][]string{nil, {"http://user:secret@127.0.0.1:9200"}} {
		if err := validateResinProxyTemplates(resinProxyStrategy, proxies); err == nil {
			t.Fatalf("invalid Resin templates accepted: %v", proxies)
		}
	}
	if err := validateResinProxyTemplates("round_robin", []string{"http://user:secret@127.0.0.1:9200"}); err != nil {
		t.Fatalf("static strategy should not require a template: %v", err)
	}
}

func TestRestoreMaskedProxyList(t *testing.T) {
	existing := []string{"http://node.{uuid}:secret@127.0.0.1:9200"}
	masked := maskProxyURL(existing[0])
	if strings.Contains(masked, "secret") || !strings.Contains(masked, "node.{uuid}") {
		t.Fatalf("unexpected masked template %q", masked)
	}
	restored := restoreMaskedProxyList([]string{masked}, existing)
	if len(restored) != 1 || restored[0] != existing[0] {
		t.Fatalf("masked proxy was not restored: %v", restored)
	}
}

func TestClineResinTransportUsesFreshProxyIdentity(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	identities := make(chan string, 2)
	errors := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				errors <- err
				return
			}
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			br := bufio.NewReader(conn)
			req, err := http.ReadRequest(br)
			if err != nil {
				conn.Close()
				errors <- err
				return
			}
			encoded := strings.TrimPrefix(req.Header.Get("Proxy-Authorization"), "Basic ")
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				conn.Close()
				errors <- err
				return
			}
			identities <- strings.SplitN(string(decoded), ":", 2)[0]
			_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
			if _, err := http.ReadRequest(br); err != nil {
				conn.Close()
				errors <- err
				return
			}
			_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			conn.Close()
		}
	}()

	template := fmt.Sprintf("http://node.{uuid}:secret@%s", listener.Addr())
	setClineProxyTestConfig(t, []string{template}, resinProxyStrategy)
	transport := &clineProxyRoundTripper{base: http.DefaultTransport}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/check", nil)
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	first, second := <-identities, <-identities
	if first == second || !strings.HasPrefix(first, "node.") || !strings.HasPrefix(second, "node.") {
		t.Fatalf("expected two fresh Resin identities, got %q and %q", first, second)
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}

func TestZenResinTransportIsRequestScoped(t *testing.T) {
	resetZenTestState(t)
	zenConfigMu.Lock()
	zenConfig = &zenConfigData{
		Enabled:       true,
		Key:           "public",
		BaseURL:       zenAPIBase,
		Proxies:       []string{"http://node.{uuid}:secret@127.0.0.1:9200"},
		ProxyStrategy: resinProxyStrategy,
	}
	zenConfigMu.Unlock()

	first, err := zenTransportForRequest()
	if err != nil {
		t.Fatal(err)
	}
	defer first.cleanup()
	second, err := zenTransportForRequest()
	if err != nil {
		t.Fatal(err)
	}
	defer second.cleanup()
	if !first.resin || !second.resin || first.client == second.client || first.via == second.via {
		t.Fatalf("expected request-scoped Resin transports: first=%q second=%q", first.via, second.via)
	}
}
