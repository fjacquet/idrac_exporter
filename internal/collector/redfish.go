package collector

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"path"
	"strings"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/go-resty/resty/v2"
)

type RedfishSession struct {
	disabled bool
	id       string
	token    string
}

type Redfish struct {
	client   *resty.Client
	baseurl  string
	hostname string
	username string
	password string
	session  RedfishSession
}

const redfishRootPath = "/redfish/v1"

// retryIdempotent retries only idempotent GET/HEAD requests on a transport error
// or a 5xx status. It never retries 4xx (a real answer) and never retries the
// session-create POST (a retried POST could create duplicate BMC sessions). In
// resty v2 an added condition overrides the default error-retry, so this must
// itself return true on err != nil for the methods we want retried.
func retryIdempotent(r *resty.Response, err error) bool {
	if r == nil || r.Request == nil {
		return false
	}
	switch r.Request.Method {
	case http.MethodGet, http.MethodHead:
		return err != nil || r.StatusCode() >= 500
	default:
		return false
	}
}

func NewRedfish(host string, auth *config.AuthConfig) *Redfish {
	baseurl := fmt.Sprintf("%s://%s", auth.Scheme, host)
	if auth.Port > 0 {
		baseurl = fmt.Sprintf("%s:%d", baseurl, auth.Port)
	}

	// Size the connection pool to the configured concurrency when set (Phase 2c).
	// The 10/20 defaults preserve the historical unlimited behavior.
	maxIdle, maxConns := 10, 20
	if n := config.Config.Concurrency; n > 0 {
		maxIdle = int(n)
		maxConns = int(n) + 1
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: !auth.Verify, MinVersion: tls.VersionTLS12},
		MaxIdleConnsPerHost:   maxIdle,
		MaxConnsPerHost:       maxConns,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: time.Duration(config.Config.Timeout) * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := resty.New().
		SetTransport(transport).
		SetTimeout(time.Duration(config.Config.Timeout) * time.Second).
		// BMCs are reached over insecure transport by design (self-signed TLS is
		// skipped; some are plain http with basic auth), so resty's per-request
		// security warnings would be log spam here.
		SetDisableWarn(true).
		SetRetryCount(2).
		SetRetryWaitTime(200 * time.Millisecond).
		SetRetryMaxWaitTime(1 * time.Second).
		AddRetryCondition(retryIdempotent)

	return &Redfish{
		client:   client,
		baseurl:  baseurl,
		hostname: host,
		username: auth.Username,
		password: auth.Password,
		session: RedfishSession{
			disabled: auth.BasicAuth,
		},
	}
}

func (r *Redfish) DisableSession() {
	r.session.disabled = true
	r.session.token = ""
	r.session.id = ""
	log.Info("Session authentication disabled for %s due to failed creation or refresh", r.hostname)
}

func (r *Redfish) CreateSession() bool {
	if r.session.disabled {
		return false
	}

	url := fmt.Sprintf("%s/redfish/v1/SessionService/Sessions", r.baseurl)
	session := Session{
		Username: r.username,
		Password: r.password,
	}

	resp, err := r.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(&session).
		Post(url)
	if err != nil {
		log.Error("Failed to query %q: %v", url, err)
		return false
	}

	// iDRAC 8 used /redfish/v1/Sessions; newer firmware uses
	// /redfish/v1/SessionService/Sessions. Fall back on 405.
	if resp.StatusCode() == http.StatusMethodNotAllowed {
		url = fmt.Sprintf("%s/redfish/v1/Sessions", r.baseurl)
		resp, err = r.client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(&session).
			Post(url)
		if err != nil {
			r.DisableSession()
			return false
		}
	}

	if resp.StatusCode() != http.StatusCreated {
		log.Error("Unexpected status code from %q: %s", url, resp.Status())
		return false
	}

	if err := json.Unmarshal(resp.Body(), &session); err != nil {
		log.Error("Error decoding response from %q: %v", url, err)
		return false
	}

	r.session.id = session.OdataId
	r.session.token = resp.Header().Get("X-Auth-Token")

	// iLO 4
	if len(r.session.id) == 0 {
		u, err := neturl.Parse(resp.Header().Get("Location"))
		if err == nil {
			r.session.id = u.Path
		}
	}

	log.Debug("Succesfully created session: %s", path.Base(r.session.id))
	return true
}

func (r *Redfish) DeleteSession() bool {
	if len(r.session.token) == 0 {
		return true
	}

	url := fmt.Sprintf("%s%s", r.baseurl, r.session.id)
	resp, err := r.client.R().
		SetHeader("Accept", "application/json").
		SetHeader("X-Auth-Token", r.session.token).
		Delete(url)
	if err != nil {
		log.Error("Failed to query %q: %v", url, err)
		return false
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		log.Error("Unexpected status code from %q: %s", url, resp.Status())
		return false
	}

	log.Debug("Succesfully deleted session: %s", path.Base(r.session.id))
	r.session.id = ""
	r.session.token = ""

	return true
}

func (r *Redfish) RefreshSession() bool {
	if r.session.disabled {
		return false
	}

	if len(r.session.token) == 0 {
		ok := r.CreateSession()
		if !ok {
			r.DisableSession()
		}
		return ok
	}

	url := fmt.Sprintf("%s%s", r.baseurl, r.session.id)
	resp, err := r.client.R().
		SetHeader("Accept", "application/json").
		SetHeader("X-Auth-Token", r.session.token).
		Get(url)
	if err != nil {
		return false
	}

	if resp.StatusCode() == http.StatusUnauthorized || resp.StatusCode() == http.StatusNotFound {
		ok := r.CreateSession()
		if !ok {
			r.DisableSession()
		}
		return ok
	} else if resp.StatusCode() != http.StatusOK {
		log.Error("Unexpected status code %d during session refresh", resp.StatusCode())
		return false
	}

	return true
}

func (r *Redfish) Get(path string, res any) bool {
	if !strings.HasPrefix(path, redfishRootPath) {
		return false
	}

	url := fmt.Sprintf("%s%s", r.baseurl, path)
	req := r.client.R().SetHeader("Accept", "application/json")
	if len(r.session.token) > 0 {
		req.SetHeader("X-Auth-Token", r.session.token)
	} else {
		req.SetBasicAuth(r.username, r.password)
	}

	log.Debug("Querying %q", url)
	resp, err := req.Get(url)
	if err != nil {
		log.Error("Failed to query %q: %v", url, err)
		return false
	}

	if config.Trace {
		log.Info("trace: GET %s -> %d", path, resp.StatusCode())
	}

	if resp.StatusCode() != http.StatusOK {
		log.Error("Unexpected status code from %q: %s", url, resp.Status())
		return false
	}

	if config.Debug {
		log.Debug("Response from %q: %s", url, resp.Body())
	}

	// Issue #192
	body := bytes.ReplaceAll(resp.Body(), []byte("\r"), []byte(""))

	if err := json.Unmarshal(body, res); err != nil {
		log.Error("Error decoding response from %q: %v", url, err)
		return false
	}

	return true
}

func (r *Redfish) Exists(path string) bool {
	if !strings.HasPrefix(path, redfishRootPath) {
		return false
	}

	url := fmt.Sprintf("%s%s", r.baseurl, path)
	req := r.client.R().SetHeader("Accept", "application/json")
	if len(r.session.token) > 0 {
		req.SetHeader("X-Auth-Token", r.session.token)
	} else {
		req.SetBasicAuth(r.username, r.password)
	}

	resp, err := req.Head(url)
	if err != nil {
		return false
	}

	if config.Trace {
		log.Info("trace: HEAD %s -> %d", path, resp.StatusCode())
	}

	if resp.StatusCode() >= 400 && resp.StatusCode() <= 499 {
		return false
	}

	return true
}
