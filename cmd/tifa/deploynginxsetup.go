package main

// The plan of setup and remove for deploy.front nginx. See deploynginx.go for what is on the server.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// readNginx asks the server as root about nginx, certbot and the sites of Tifa. Inside a plan it asks
// once.
func (r *setupRun) readNginx(ctx context.Context) (nginxState, error) {
	return memoRead(ctx, "ssh-nginx", func() (nginxState, error) {
		var out strings.Builder
		if err := r.t.ssh(ctx, "root", nginxProbeScript(), nil, &out, r.stderr); err != nil {
			return nginxState{}, err
		}
		return parseNginxState(out.String())
	})
}

// rootScript is a step that runs script as root and says what it printed. A script that fails
// says why on stderr, such as what nginx said of a site: that is the error.
func (r *setupRun) rootScript(script string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var out, said strings.Builder
		if err := r.t.ssh(ctx, "root", script, nil, &out, &said); err != nil {
			if text := strings.TrimSpace(said.String()); text != "" {
				var login *sshLoginError
				if !errors.As(err, &login) {
					// The scripts begin what they say with "tifa: ", which the error adds itself.
					return "", errors.New(strings.TrimPrefix(text, "tifa: "))
				}
			}
			return "", err
		}
		return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(out.String()), "\n", ", ")), nil
	}
}

func nginxSiteText(app, host string, port int) string {
	who := pick(host == "", "the visitors that name no domain", host)
	return fmt.Sprintf("nginx: the site of %s -> 127.0.0.1:%d in /etc/nginx/conf.d/tifa-%s.conf, which nginx checks with nginx -t before it reloads", who, port, app)
}

// nginxSteps plans nginx for an app with deploy.front nginx: the nginx and certbot of Tifa, the site
// of the app, its certificate, and the ports in ufw. It stops, before anything changes, for an nginx
// that is not Tifa's or a program on the ports. The app is planned before it: its loopback port is
// what the site points at.
func (r *setupRun) nginxSteps(ctx context.Context, p *setupPlan) error {
	t := r.t
	ns, err := r.readNginx(ctx)
	if err != nil {
		return fmt.Errorf("deploy.front: nginx installs and changes nginx as root, and %s cannot be reached as root: %w", t.host, err)
	}
	if err := nginxGuard(t, ns, r.st.UserSockets); err != nil {
		return err
	}
	host, app := t.own, t.app
	// ufw comes first: Let's Encrypt must reach port 80 when certbot asks for the certificate.
	if after := r.firewallFor(ctx, p, "nginx", nginxPorts, nil); after != nil {
		after()
	}
	p.firewall = "Visitors need ports 80 and 443 of the server open. Tifa does not change your provider's firewall or security groups: if they block them, open them there."
	nginxPkgs, certbotPkgs := nginxPackages(ns, host != "")
	if (len(nginxPkgs) > 0 || len(certbotPkgs) > 0) && !ns.Apt {
		p.cannot("server: install nginx"+pick(host != "", " and certbot", ""), errNoAptForNginx.Error())
		return nil
	}
	if len(nginxPkgs) > 0 {
		p.add(opAdd, "server: install nginx with apt, which routes the visitors of the apps of this server, and write /etc/tifa/nginx to mark it as Tifa's, as root",
			r.rootScript(nginxInstallScript(nginxPkgs, nil)))
	} else {
		p.ok("nginx: installed by tifa")
	}
	if len(certbotPkgs) > 0 {
		p.add(opAdd, "server: install certbot and its nginx plugin with apt, which gets the certificates and renews them with its own timer, as root",
			r.rootScript(nginxInstallScript(nil, certbotPkgs)))
	}

	cert, hasCert := ns.cert(app)
	// A name the server could not read ("?") does not fit: the certificate is asked for again.
	certHost := hasCert && cert.Host == host
	tlsNow := host != "" && certHost
	want := nginxSiteFile(app, host, r.port, tlsNow, ns.IPv6)
	text := nginxSiteText(app, host, r.port)
	// The port is read when the step runs: the app step before it may have taken another one than
	// the plan guessed.
	apply := func(ctx context.Context) (string, error) {
		return r.rootScript(nginxSiteScript(app, nginxSiteFile(app, host, r.port, tlsNow, ns.IPv6)))(ctx)
	}
	cur, has := ns.site(app)
	switch {
	case !has:
		p.add(opAdd, text, apply)
	case cur.Sum != sha(want) || ns.Default:
		p.add(opChange, text+pick(ns.Default, ", and the welcome site of nginx goes", ""), apply)
	case ns.installed() && !ns.Active && len(nginxPkgs) == 0:
		// The site is as it should be, but nginx does not run: a start that failed before, or a
		// stop by hand.
		p.add(opChange, "nginx: start nginx, which is installed and not running, after nginx -t", r.rootScript(nginxStartScript()))
	default:
		p.ok(text)
	}

	if host != "" {
		certText := fmt.Sprintf("server: the certificate of %s from Let's Encrypt with certbot, which renews it with its own timer, and HTTPS in the site of %s", host, app)
		switch {
		case certHost:
			p.ok(fmt.Sprintf("server: the certificate of %s from Let's Encrypt, renewed by certbot", host))
		default:
			replace := hasCert
			op := opAdd
			if replace {
				op = opChange
			}
			p.add(op, certText, func(ctx context.Context) (string, error) {
				if _, err := r.rootScript(nginxCertScript(app, host, replace))(ctx); err != nil {
					return "", err
				}
				return r.rootScript(nginxSiteScript(app, nginxSiteFile(app, host, r.port, true, ns.IPv6)))(ctx)
			})
		}
	}

	return nil
}

// nginxGoneSteps plans, for an app whose deploy.front is not nginx, the removal of what an earlier
// nginx front left: its site and certificate, and nginx once no app uses it. They come before the
// app moves, since nginx may hold ports 80 and 443 that the app needs.
func (r *setupRun) nginxGoneSteps(ctx context.Context, p *setupPlan) error {
	if !r.haveServer || !r.st.NginxTifa {
		return nil
	}
	return r.nginxRemovalSteps(ctx, p, ", since deploy.front is not nginx any more")
}

// nginxRemovalSteps plans the removal of the site and certificate of the app, and of nginx and
// certbot, the ufw rules and the marker when no other app has a site. It reads nginx as root. The
// steps run in the order nginx needs: the site first, so nginx no longer asks for the certificate.
func (r *setupRun) nginxRemovalSteps(ctx context.Context, p *setupPlan, why string) error {
	app := r.t.app
	ns, err := r.readNginx(ctx)
	if err != nil {
		p.cannot("nginx: the site and certificate of "+app+", and nginx once no app uses it",
			fmt.Sprintf("only root can change nginx, but %s cannot be reached as root: %v", r.t.host, err))
		return nil
	}
	if !ns.Tifa {
		return nil
	}
	// What comes after the site depends on it: the configuration on disk still names the certificate
	// while the site is in nginx, and the packages and the ports are what the sites of nginx use. A
	// site that nginx did not let go stays, and then so does all of that.
	siteStays, stayWhy := false, fmt.Sprintf("the site of %s is still in nginx: run the command again once nginx takes the configuration without it", app)
	guard := func(do func(context.Context) (string, error)) func(context.Context) (string, error) {
		return func(ctx context.Context) (string, error) {
			if siteStays {
				return "", errors.New("kept, since " + stayWhy)
			}
			return do(ctx)
		}
	}
	if _, ok := ns.site(app); ok {
		run := r.rootScript(nginxRemoveSiteScript(app))
		p.add(opRemove, fmt.Sprintf("nginx: the site of %s in /etc/nginx/conf.d/tifa-%s.conf, which tifa wrote%s, and nginx reloads after nginx -t", app, app, why),
			func(ctx context.Context) (string, error) {
				res, err := run(ctx)
				siteStays = siteStays || err != nil
				return res, err
			})
	}
	if c, ok := ns.cert(app); ok {
		p.add(opRemove, fmt.Sprintf("server: the certificate tifa-%s of %s, made by certbot%s", app, pick(c.Host == "?", "the site", c.Host), why),
			guard(r.rootScript(nginxRemoveCertScript(app))))
	}
	if len(ns.otherSites(app)) > 0 {
		return nil
	}
	what := "nginx"
	if slices.Contains(ns.TifaCerts, "certbot") {
		what = "nginx and certbot"
	} else if len(ns.TifaCerts) > 0 {
		what = "nginx and the nginx plugin of certbot"
	}
	p.add(opRemove, fmt.Sprintf("server: uninstall %s, which setup installed, with their configuration, and delete /etc/tifa/nginx, as root, since no other app uses nginx", what),
		guard(func(ctx context.Context) (string, error) {
			res, err := r.rootScript(nginxUninstallScript())(ctx)
			// A site came while this ran: nginx stays, and so do its ports in ufw.
			if err == nil && strings.HasPrefix(res, "kept nginx") {
				siteStays, stayWhy = true, "nginx has a site again: "+res
			}
			return res, err
		}))
	if after := r.firewallFor(ctx, p, "nginx", nil, nginxPorts); after != nil {
		n := len(p.steps)
		after()
		for i := n; i < len(p.steps); i++ {
			p.steps[i].do = guard(p.steps[i].do)
		}
	}
	return nil
}

// printNginxVisitors ends a setup of an app behind nginx: which ports visitors need, and that Tifa
// does not open them at the provider. Once a release runs, this machine asks each port.
func (r *setupRun) printNginxVisitors(ctx context.Context, w io.Writer) {
	if !r.haveServer {
		return
	}
	ports := []int{httpPort}
	if r.t.own != "" {
		ports = nginxPorts
	}
	them := pick(len(ports) > 1, "them", "it")
	line := fmt.Sprintf("%s is served by nginx on %s of the server. If the firewall or a security group of your provider blocks %s, open %s there: Tifa does not change those.",
		r.t.app, portList(ports), them, them)
	if r.st.Current != "" {
		host := r.t.serverAddress(ctx)
		if r.t.own != "" {
			host = r.t.own
		}
		var closed []string
		for _, q := range ports {
			if !portAnswers(ctx, host, q) {
				closed = append(closed, strconv.Itoa(q))
			}
		}
		if len(closed) == 0 {
			line += " " + pick(len(ports) > 1, "Both answer", "It answers") + " from this machine."
		} else {
			line += fmt.Sprintf(" %s %s did not answer from this machine within 3 seconds.", pick(len(closed) > 1, "Ports", "Port"), strings.Join(closed, " and "))
		}
	}
	fmt.Fprintln(w, line)
}
