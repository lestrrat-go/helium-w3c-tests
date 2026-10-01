package xslt3_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const (
	// w3cRemoteFixtureDir holds the local copies of every http(s) resource the
	// xslt30 cases fetch. `go run ./cmd/w3cgen fetch xslt30` overlays it from the
	// committed fixtures/xslt30/remote tree, laid out as <host>/<path>.
	w3cRemoteFixtureDir = w3cTestdataDir + "/remote"

	// w3cOriginalURLHeader carries the URL the case asked for from the client
	// transport to the local server, which only ever sees its own address.
	w3cOriginalURLHeader = "X-W3c-Original-Url"

	// w3cRemoteErrorHeader carries the local server's reason for a non-200
	// answer back to the client transport, which reports it as a test error.
	w3cRemoteErrorHeader = "X-W3c-Remote-Error"
)

// w3cRemoteResource is the local copy of one http(s) resource.
type w3cRemoteResource struct {
	file        string // slash-separated path under w3cRemoteFixtureDir
	contentType string // sent as Content-Type; unparsed-text() reads its charset
}

// w3cRemoteResources maps every http(s) URL the xslt30 cases fetch, exactly as
// helium requests it, to its local copy. The harness never reaches the
// network: a URL missing from this map gets a 404 and fails the case that
// fetched it, so a new network dependency shows up as a test failure.
var w3cRemoteResources = map[string]w3cRemoteResource{
	// match-035: the source document's xsi:schemaLocation hint, which helium
	// loads on every transform. The case does not depend on the schema's
	// content, so this is a minimal schema for the hinted namespace. The real
	// schema also imports http://www.loc.gov/standards/xlink/xlink.xsd, which the
	// stand-in does not.
	"http://www.loc.gov/ead/ead.xsd": {
		file:        "www.loc.gov/ead/ead.xsd",
		contentType: "application/xml",
	},
	// unparsed-text-2002 (https) and unparsed-text-2003 (http). 2002 expects
	// the page NOT to contain the phrase it searches for, and 2003 only checks
	// that the page is retrievable, so this is a small UTF-8 stand-in.
	"http://www.w3.org/Consortium/mission.html": {
		file:        "www.w3.org/Consortium/mission.html",
		contentType: "text/html; charset=UTF-8",
	},
	"https://www.w3.org/Consortium/mission.html": {
		file:        "www.w3.org/Consortium/mission.html",
		contentType: "text/html; charset=UTF-8",
	},
	// unparsed-text-2002: the case decodes the page with the charset from the
	// HTTP Content-Type header (iso-8859-1) and expects it to contain
	// "Håkon Lie, W3C", so this is the real W3C Working Draft, byte for byte,
	// served with the live server's Content-Type. W3C Document License.
	"https://www.w3.org/TR/1999/WD-font-19990902": {
		file:        "www.w3.org/TR/1999/WD-font-19990902.html",
		contentType: "text/html; charset=iso-8859-1",
	},
}

// w3cRemoteServer is the shared local server for w3cRemoteResources and the
// transport that reaches it. It starts on first use and lives for the whole
// test binary.
type w3cRemoteServer struct {
	srv       *httptest.Server
	transport *http.Transport
}

var w3cRemoteServerOnce = sync.OnceValue(w3cStartRemoteServer)

func w3cStartRemoteServer() *w3cRemoteServer {
	srv := httptest.NewServer(w3cRemoteHandler{})
	return &w3cRemoteServer{
		srv: srv,
		// A zero Transport has no Proxy, so proxy environment variables cannot
		// send requests anywhere else either.
		transport: &http.Transport{},
	}
}

// w3cRemoteHandler serves w3cRemoteResources, keyed by the original URL in
// w3cOriginalURLHeader.
type w3cRemoteHandler struct{}

func (w3cRemoteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	orig := r.Header.Get(w3cOriginalURLHeader)
	res, ok := w3cRemoteResources[orig]
	if !ok {
		w3cRemoteError(w, http.StatusNotFound, "no local copy of "+orig+"; add one to w3cRemoteResources (xslt3/w3c_http_test.go)")
		return
	}
	data, err := os.ReadFile(filepath.Join(w3cRemoteFixtureDir, filepath.FromSlash(res.file)))
	if err != nil {
		w3cRemoteError(w, http.StatusInternalServerError, "read local copy of "+orig+": "+err.Error()+"; run go run ./cmd/w3cgen fetch xslt30")
		return
	}
	w.Header().Set("Content-Type", res.contentType)
	_, _ = w.Write(data)
}

func w3cRemoteError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set(w3cRemoteErrorHeader, msg)
	http.Error(w, msg, code)
}

// w3cLocalTransport sends every request, whatever its scheme and host, to the
// local server, and reports a non-200 answer as an error on the test that made
// the request. helium can treat a failed fetch as a soft miss (an unloadable
// xsi:schemaLocation hint, unparsed-text-available()), so the response alone
// would not always fail the case.
type w3cLocalTransport struct {
	t      *testing.T
	server *w3cRemoteServer
}

func (lt w3cLocalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	orig := req.URL.String()
	local := req.Clone(req.Context())
	local.URL.Scheme = "http"
	local.URL.Host = lt.server.srv.Listener.Addr().String()
	local.Host = req.URL.Host
	local.Header.Set(w3cOriginalURLHeader, orig)
	resp, err := lt.server.transport.RoundTrip(local)
	if err != nil {
		lt.t.Errorf("local HTTP server request for %s: %v", orig, err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		lt.t.Errorf("local HTTP server answered %d for %s: %s", resp.StatusCode, orig, resp.Header.Get(w3cRemoteErrorHeader))
	}
	return resp, nil
}

// w3cTestHTTPClient returns the HTTP client for one case. It serves every
// http(s) fetch from w3cRemoteResources through the local server and never
// opens a connection to any other host.
func w3cTestHTTPClient(t *testing.T) *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: w3cLocalTransport{t: t, server: w3cRemoteServerOnce()},
	}
}
