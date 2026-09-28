package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type proxyConfigResponse struct {
	Data struct {
		Proxies []string `json:"proxies"`
	} `json:"data"`
}

func decodeProxyConfigResponse(t *testing.T, rr *httptest.ResponseRecorder) proxyConfigResponse {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response proxyConfigResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestOpenCodeConfigReturnsPlaintextProxy(t *testing.T) {
	proxy := "http://node.{uuid}:secret@127.0.0.1:9200"
	zenConfigMu.Lock()
	old := zenConfig
	zenConfig = defaultZenConfig()
	zenConfig.Proxies = []string{proxy}
	zenConfig.ProxyStrategy = resinProxyStrategy
	zenConfigMu.Unlock()
	t.Cleanup(func() {
		zenConfigMu.Lock()
		zenConfig = old
		zenConfigMu.Unlock()
	})

	rr := httptest.NewRecorder()
	handleOpenCodeConfig(rr, httptest.NewRequest(http.MethodGet, "/admin/api/opencode/config", nil))
	response := decodeProxyConfigResponse(t, rr)
	if len(response.Data.Proxies) != 1 || response.Data.Proxies[0] != proxy {
		t.Fatalf("proxies = %v, want plaintext proxy", response.Data.Proxies)
	}
}

func TestClineConfigReturnsPlaintextProxy(t *testing.T) {
	proxy := "http://node.{uuid}:secret@127.0.0.1:9200"
	clineProxyCfgMu.Lock()
	old := clineProxyCfg
	clineProxyCfg = &clineProxyConfigData{Proxies: []string{proxy}, ProxyStrategy: resinProxyStrategy}
	clineProxyCfgMu.Unlock()
	t.Cleanup(func() {
		clineProxyCfgMu.Lock()
		clineProxyCfg = old
		clineProxyCfgMu.Unlock()
	})

	rr := httptest.NewRecorder()
	handleClineProxyConfig(rr, httptest.NewRequest(http.MethodGet, "/admin/api/cline-proxy/config", nil))
	response := decodeProxyConfigResponse(t, rr)
	if len(response.Data.Proxies) != 1 || response.Data.Proxies[0] != proxy {
		t.Fatalf("proxies = %v, want plaintext proxy", response.Data.Proxies)
	}
}
