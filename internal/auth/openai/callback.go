// Copyright 2026 DeMarco
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package openai

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CallbackResult is the authorization code delivered to the localhost server.
type CallbackResult struct {
	Code  string
	State string
	Err   error
}

// CallbackServer is a one-shot localhost OAuth redirect listener.
type CallbackServer struct {
	Host  string
	Ports []int
	Path  string
	State string

	listener    net.Listener
	server      *http.Server
	extraServer *http.Server
	once        sync.Once
	result      chan CallbackResult
}

func NewCallbackServer() *CallbackServer {
	return &CallbackServer{
		Host:  callbackHost,
		Ports: append([]int(nil), callbackPorts...),
		Path:  callbackPath,
	}
}

// Start binds the first available port and begins serving the callback.
func (s *CallbackServer) Start() (redirectURI string, err error) {
	if s == nil {
		return "", errors.New("callback server is nil")
	}
	if strings.TrimSpace(s.Host) == "" {
		s.Host = callbackHost
	}
	if strings.TrimSpace(s.Path) == "" {
		s.Path = callbackPath
	}
	if !strings.HasPrefix(s.Path, "/") {
		s.Path = "/" + s.Path
	}
	if strings.TrimSpace(s.State) == "" {
		state, err := randomState()
		if err != nil {
			return "", err
		}
		s.State = state
	}
	if s.result == nil {
		s.result = make(chan CallbackResult, 1)
	}
	listener, err := listenFirst(s.Host, s.Ports)
	if err != nil {
		return "", err
	}
	s.listener = listener
	mux := http.NewServeMux()
	mux.HandleFunc("/cancel", s.handleCancel)
	mux.HandleFunc(s.Path, s.handle)
	if !strings.HasSuffix(s.Path, "/") {
		mux.HandleFunc(s.Path+"/", s.handle)
	}
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = s.server.Serve(listener)
	}()
	s.serveLoopbackCompanion(mux)
	return RedirectURI(s.Host, listener.Addr(), s.Path), nil
}

// Wait blocks until a matching callback arrives or ctx ends.
func (s *CallbackServer) Wait(ctx context.Context) (CallbackResult, error) {
	if s == nil || s.result == nil {
		return CallbackResult{}, errors.New("callback server is not running")
	}
	select {
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	case result := <-s.result:
		return result, result.Err
	}
}

// Close shuts down the listener.
func (s *CallbackServer) Close() error {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var first error
	if s.extraServer != nil {
		if err := s.extraServer.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *CallbackServer) handle(writer http.ResponseWriter, request *http.Request) {
	query := callbackParams(request)
	if errText := strings.TrimSpace(query.Get("error")); errText != "" {
		desc := strings.TrimSpace(query.Get("error_description"))
		if desc == "" {
			desc = errText
		}
		s.finish(writer, http.StatusBadRequest, "Login failed: "+desc, CallbackResult{Err: errors.New(desc)})
		return
	}
	code := strings.TrimSpace(query.Get("code"))
	state := strings.TrimSpace(query.Get("state"))
	if code == "" {
		// Prefetch, favicon-adjacent hits, and hash-only redirects must not
		// consume the one-shot result. A small page lifts `#code=` into the query.
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, fragmentRecoveryHTML)
		return
	}
	// ChatGPT sometimes omits or rewrites `state` (Safari ITP, simplified
	// flow, a leftover listener on :1455). This server is localhost and
	// one-shot; PKCE still binds the code to this login.
	s.finish(writer, http.StatusOK, "Login complete. You can close this window.", CallbackResult{Code: code, State: state})
}

func (s *CallbackServer) handleCancel(writer http.ResponseWriter, _ *http.Request) {
	s.finish(writer, http.StatusOK, "Login cancelled", CallbackResult{Err: errors.New("login cancelled")})
}

func (s *CallbackServer) finish(writer http.ResponseWriter, status int, message string, result CallbackResult) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = fmt.Fprintf(writer, "<!doctype html><title>gxx</title><p>%s</p>", htmlEscape(message))
	s.once.Do(func() {
		s.result <- result
	})
}

func (s *CallbackServer) serveLoopbackCompanion(handler http.Handler) {
	if s == nil || s.listener == nil {
		return
	}
	tcp, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok || tcp.Port <= 0 {
		return
	}
	companionHost := "::1"
	if ip := tcp.IP; ip != nil && ip.To4() == nil {
		companionHost = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(companionHost, strconv.Itoa(tcp.Port)))
	if err != nil {
		return
	}
	s.extraServer = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = s.extraServer.Serve(ln)
	}()
}

func callbackParams(request *http.Request) url.Values {
	values := request.URL.Query()
	if request.Method == http.MethodPost {
		_ = request.ParseForm()
		for key, list := range request.PostForm {
			if values.Get(key) == "" && len(list) > 0 {
				values.Set(key, list[0])
			}
		}
	}
	return values
}

func htmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(value)
}

func listenFirst(host string, ports []int) (net.Listener, error) {
	if len(ports) == 0 {
		ports = callbackPorts
	}
	var last error
	for i, port := range ports {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return listener, nil
		}
		last = err
		if port > 0 && i == 0 {
			cancelLoginPort(host, port)
			time.Sleep(200 * time.Millisecond)
			listener, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
			if err == nil {
				return listener, nil
			}
			last = err
		}
	}
	if last == nil {
		return nil, errors.New("no callback ports configured")
	}
	return nil, fmt.Errorf("listen for OAuth callback: %w", last)
}

func cancelLoginPort(host string, port int) {
	if port <= 0 {
		return
	}
	if strings.TrimSpace(host) == "" {
		host = callbackHost
	}
	client := &http.Client{Timeout: 400 * time.Millisecond}
	endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/cancel"
	resp, err := client.Get(endpoint)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// RedirectURI builds the browser redirect target. OAuth clients register
// localhost, so the host in the URI is localhost even when we bind 127.0.0.1.
func RedirectURI(bindHost string, addr net.Addr, path string) string {
	host := "localhost"
	port := ""
	if tcp, ok := addr.(*net.TCPAddr); ok {
		port = strconv.Itoa(tcp.Port)
	} else {
		_, port, _ = net.SplitHostPort(addr.String())
	}
	if strings.TrimSpace(path) == "" {
		path = callbackPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	_ = bindHost
	return "http://" + net.JoinHostPort(host, port) + path
}

func randomState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

const fragmentRecoveryHTML = `<!doctype html><title>gxx</title>
<script>
(function(){
  var h=(location.hash||"").replace(/^#/,"");
  if(h&&(h.indexOf("code=")>=0||h.indexOf("error=")>=0)){
    location.replace(location.pathname+"?"+h);
  }
})();
</script>
<p>Waiting for login…</p>`
