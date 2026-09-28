package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
)

const resinProxyStrategy = "resin"

func proxyStrategyValid(strategy string) bool {
	switch strategy {
	case "round_robin", "random", "fill", resinProxyStrategy:
		return true
	default:
		return false
	}
}

func hasProxyTemplate(raw string) bool {
	username, ok := proxyTemplateUsername(raw)
	if !ok {
		return false
	}
	return strings.Contains(username, "{uuid}") || strings.Contains(username, "{uuid_hex}")
}

func proxyTemplateUsername(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	schemeEnd := strings.Index(raw, "://")
	if schemeEnd < 0 {
		return "", false
	}
	authorityStart := schemeEnd + 3
	at := strings.LastIndex(raw[authorityStart:], "@")
	if at < 0 {
		return "", false
	}
	userinfo := raw[authorityStart : authorityStart+at]
	username := strings.SplitN(userinfo, ":", 2)[0]
	if decoded, err := url.PathUnescape(username); err == nil {
		username = decoded
	}
	return username, true
}

func validateResinProxyTemplates(strategy string, proxies []string) error {
	if strategy != resinProxyStrategy {
		return nil
	}
	if len(proxies) == 0 {
		return fmt.Errorf("resin proxy strategy requires at least one proxy template")
	}
	for _, raw := range proxies {
		if !hasProxyTemplate(raw) {
			return fmt.Errorf("resin proxy template %q must contain {uuid} or {uuid_hex} in the username", maskProxyURL(raw))
		}
	}
	return nil
}

func renderProxyTemplate(raw string) (string, error) {
	if !hasProxyTemplate(raw) {
		return "", fmt.Errorf("proxy template must contain {uuid} or {uuid_hex} in the username")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate proxy session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hex := fmt.Sprintf("%x", b)
	uuid := fmt.Sprintf("%s-%s-%s-%s-%s", hex[0:8], hex[8:12], hex[12:16], hex[16:20], hex[20:32])
	rendered := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "{uuid_hex}", hex), "{uuid}", uuid)
	if _, err := url.Parse(rendered); err != nil {
		return "", fmt.Errorf("parse rendered proxy template: %w", err)
	}
	return rendered, nil
}

// restoreMaskedProxyList keeps existing credentials when the admin page submits
// the exact masked representation returned by the GET endpoint.
func restoreMaskedProxyList(submitted, existing []string) []string {
	byMasked := make(map[string]string, len(existing))
	for _, raw := range existing {
		byMasked[maskProxyURL(raw)] = raw
	}
	out := make([]string, 0, len(submitted))
	for _, raw := range submitted {
		if original, ok := byMasked[raw]; ok {
			out = append(out, original)
		} else {
			out = append(out, raw)
		}
	}
	return out
}

type cleanupReadCloser struct {
	io.ReadCloser
	once    sync.Once
	cleanup func()
}

func (r *cleanupReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(r.cleanup)
	return err
}
