package devserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/deverrors"
)

// maxBrowserReport bounds the body of an error a page reports. The dev client cuts longer texts.
const maxBrowserReport = 128 << 10

// askAppWait bounds how long tifa dev waits for the app to answer for its errors or forget them.
const askAppWait = 5 * time.Second

// testHookCollectErrors lets tests act between the wait for a build and the reading of a report.
var testHookCollectErrors func()

// serveErrors answers `tifa errors` with the errors since the last build started: the failure the
// overlay shows, the request errors and render warnings of the app and the errors pages reported.
// During a build it waits for the build to end. A build that starts while the report is read
// drops what it read, so the report waits for that build too and is read again.
func (d *dev) serveErrors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(deverrors.PIDHeader, strconv.Itoa(os.Getpid()))
	if !plainHost(r.Host) {
		http.Error(w, "tifa dev answers only under localhost or an IP address", http.StatusForbidden)
		return
	}
	select {
	case <-d.first:
	case <-r.Context().Done():
		return
	}
	for {
		started, building := d.buildState()
		if building != nil {
			select {
			case <-building:
				continue
			case <-r.Context().Done():
				return
			}
		}
		if testHookCollectErrors != nil {
			testHookCollectErrors()
		}
		report, err := d.collectErrors(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if again, _ := d.buildState(); again != started {
			continue
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(report.Normal())
		return
	}
}

// collectErrors reads the errors of the browser, of the app and the failure the overlay shows.
func (d *dev) collectErrors(ctx context.Context) (deverrors.Report, error) {
	report := deverrors.Report{Browser: d.browser.List()}
	if d.running() {
		app, err := d.askApp(ctx)
		switch {
		case err == nil:
			report.Requests, report.Renders = app.Requests, app.Renders
		case d.running():
			return report, fmt.Errorf("asking the app for its errors: %w", err)
		}
		// An app that stopped meanwhile has no errors to give, and its exit is the failure below.
	}
	if st := d.hub.current(); st.Error != "" {
		report.Failure = &deverrors.Failure{Title: st.Title, Error: st.Error}
	}
	return report, nil
}

// takeBrowserError keeps an error a page of tifa dev reports, see js/dev/client.js. Only a page
// of tifa dev itself can send one: the origin must be the one the request went to, under
// localhost or an IP address, which a site that points its own name at this machine cannot use.
func (d *dev) takeBrowserError(w http.ResponseWriter, r *http.Request) {
	if !plainHost(r.Host) || !sameOrigin(r) {
		http.Error(w, "tifa dev takes errors only from its own pages", http.StatusForbidden)
		return
	}
	// The whole body is read, so that the limit and the check of the JSON cover all of it.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBrowserReport))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		http.Error(w, fmt.Sprintf("an error report takes at most %d bytes", maxBrowserReport), http.StatusRequestEntityTooLarge)
		return
	case err != nil:
		http.Error(w, "reading the error report: "+err.Error(), http.StatusBadRequest)
		return
	}
	var e deverrors.BrowserReport
	// Unmarshal takes one value and white space around it, nothing after it.
	if err := json.Unmarshal(body, &e); err != nil {
		http.Error(w, "the error report is not one JSON object: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch e.Kind {
	case "recoverable", "uncaught", "rejection":
	default:
		http.Error(w, fmt.Sprintf("unknown kind %q", e.Kind), http.StatusBadRequest)
		return
	}
	d.browser.Add(e)
	w.WriteHeader(http.StatusNoContent)
}

// forgetErrors drops the errors from before a build: those of the browser, and those of the app,
// which keeps serving when the build fails.
func (d *dev) forgetErrors(ctx context.Context) {
	d.browser.Clear()
	if !d.running() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, askAppWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://"+d.appAddr+deverrors.Path, nil)
	if err != nil {
		panic(err) // the URL is always valid
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			err = errors.New(resp.Status)
		}
	}
	if err != nil && ctx.Err() == nil && d.running() {
		d.log.Printf("the app did not forget its errors of the last build: %v", err)
	}
}

// askApp gets the request errors and render warnings of the running app.
func (d *dev) askApp(ctx context.Context) (deverrors.App, error) {
	ctx, cancel := context.WithTimeout(ctx, askAppWait)
	defer cancel()
	var app deverrors.App
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+d.appAddr+deverrors.Path, nil)
	if err != nil {
		return app, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return app, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return app, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	err = json.NewDecoder(resp.Body).Decode(&app)
	return app, err
}

// plainHost reports whether host, the Host of a request, is localhost or an IP address, with or
// without a port.
func plainHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
	return name == "localhost" || net.ParseIP(name) != nil
}

// sameOrigin reports whether the request comes from a page of the origin it goes to.
func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && origin.Scheme == "http" && origin.Host == r.Host
}
