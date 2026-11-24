// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"codedistill/internal/api"
	"codedistill/internal/storage/sqlite"
)

// How people sign in to a multi-user server (docs/design/sign-in.md). One way
// per installation, set when it is installed, by environment:
//
//   - an identity provider over OpenID Connect: CODEDISTILL_OIDC_ISSUER,
//     _CLIENT_ID, _CLIENT_SECRET, _REDIRECT_URL;
//   - or a proxy in front of the server that signs who each request is from:
//     CODEDISTILL_PROXY_AUTH = aws-alb | google-iap | cloudflare-access, with
//     CODEDISTILL_PROXY_AUDIENCE, _TEAM, _ALB_ARN, _REGION, _LOGOUT_URL as
//     that proxy needs.
//
// Both: CODEDISTILL_ADMIN_EMAIL (the first owner), CODEDISTILL_ALLOWED_DOMAINS,
// and for identity providers that do not mark emails verified,
// CODEDISTILL_OIDC_TRUST_EMAIL=1.
//
// Neither set: single-user. Either set but wrong: the server does not start.
// It used to fall back to single-user, which looked like it worked while
// nobody could sign in.

type signInConfig struct {
	mode string // "", "oidc" or "proxy"

	issuer, clientID, clientSecret, redirect string
	proxy                                    api.ProxyConfig

	adminEmail string
	domains    []string
	trustEmail bool
}

// readSignIn reads the sign-in configuration from the environment and checks
// it is complete. It reaches no network.
func readSignIn() (signInConfig, error) {
	c := signInConfig{
		issuer:       strings.TrimSpace(os.Getenv("CODEDISTILL_OIDC_ISSUER")),
		clientID:     strings.TrimSpace(os.Getenv("CODEDISTILL_OIDC_CLIENT_ID")),
		clientSecret: os.Getenv("CODEDISTILL_OIDC_CLIENT_SECRET"),
		redirect:     strings.TrimSpace(os.Getenv("CODEDISTILL_OIDC_REDIRECT_URL")),
		proxy: api.ProxyConfig{
			Kind:      strings.TrimSpace(os.Getenv("CODEDISTILL_PROXY_AUTH")),
			Audience:  strings.TrimSpace(os.Getenv("CODEDISTILL_PROXY_AUDIENCE")),
			Team:      strings.TrimSpace(os.Getenv("CODEDISTILL_PROXY_TEAM")),
			ALBARN:    strings.TrimSpace(os.Getenv("CODEDISTILL_PROXY_ALB_ARN")),
			Region:    strings.TrimSpace(os.Getenv("CODEDISTILL_PROXY_REGION")),
			LogoutURL: strings.TrimSpace(os.Getenv("CODEDISTILL_PROXY_LOGOUT_URL")),
		},
		adminEmail: strings.TrimSpace(os.Getenv("CODEDISTILL_ADMIN_EMAIL")),
		trustEmail: os.Getenv("CODEDISTILL_OIDC_TRUST_EMAIL") == "1",
	}
	for _, d := range strings.Split(os.Getenv("CODEDISTILL_ALLOWED_DOMAINS"), ",") {
		if d = strings.TrimSpace(d); d != "" {
			c.domains = append(c.domains, d)
		}
	}
	switch {
	case c.issuer != "" && c.proxy.Kind != "":
		return c, errors.New("both CODEDISTILL_OIDC_ISSUER and CODEDISTILL_PROXY_AUTH are set: a server signs people in one way — choose one")
	case c.issuer != "":
		c.mode = "oidc"
		var missing []string
		if c.clientID == "" {
			missing = append(missing, "CODEDISTILL_OIDC_CLIENT_ID")
		}
		if c.redirect == "" {
			missing = append(missing, "CODEDISTILL_OIDC_REDIRECT_URL")
		}
		if len(missing) > 0 {
			return c, fmt.Errorf("CODEDISTILL_OIDC_ISSUER is set but %s %s not", strings.Join(missing, " and "), isAre(len(missing)))
		}
	case c.proxy.Kind != "":
		c.mode = "proxy"
	}
	return c, nil
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// applySignIn configures the server's sign-in from c, reaching the identity
// provider to load its configuration. An error means the server must not
// start: half-configured sign-in is worse than none.
func applySignIn(ctx context.Context, srv *api.Server, c signInConfig) (*api.Server, error) {
	switch c.mode {
	case "oidc":
		auth, err := api.NewOIDCAuth(ctx, c.issuer, c.clientID, c.clientSecret, c.redirect)
		if err != nil {
			return srv, fmt.Errorf("sign-in through %s: %w", c.issuer, err)
		}
		srv = srv.WithAuth(auth).WithTrustedEmail(c.trustEmail)
	case "proxy":
		p, err := api.NewProxyAuth(ctx, c.proxy)
		if err != nil {
			return srv, fmt.Errorf("sign-in through the proxy: %w", err)
		}
		srv = srv.WithProxyAuth(p)
	default:
		return srv, nil
	}
	return srv.WithMembershipPolicy(c.adminEmail, c.domains), nil
}

// cmdAuthCheck is `codedistill auth check`: whether this server's sign-in is
// configured and reachable, for whoever installs it, before anyone tries to
// sign in.
func cmdAuthCheck(args []string) error {
	fs := flag.NewFlagSet("auth check", flag.ContinueOnError)
	dbPath := fs.String("db", "", "also report the server's owners and seats from this database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	problems := 0
	ok := func(f string, a ...any) { fmt.Printf("  ok     "+f+"\n", a...) }
	warn := func(f string, a ...any) { fmt.Printf("  warn   "+f+"\n", a...) }
	bad := func(f string, a ...any) { problems++; fmt.Printf("  FAIL   "+f+"\n", a...) }

	c, err := readSignIn()
	fmt.Println("Sign-in")
	switch {
	case err != nil:
		bad("%v", err)
	case c.mode == "":
		ok("none configured: a single-user server (set CODEDISTILL_OIDC_ISSUER or CODEDISTILL_PROXY_AUTH for multi-user)")
	case c.mode == "oidc":
		ok("identity provider %s", c.issuer)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := api.NewOIDCAuth(ctx, c.issuer, c.clientID, c.clientSecret, c.redirect); err != nil {
			bad("cannot load the provider's configuration: %v", err)
		} else {
			ok("provider reachable; client %s; returns to %s", c.clientID, c.redirect)
		}
		if c.clientSecret == "" {
			warn("CODEDISTILL_OIDC_CLIENT_SECRET is empty: fine for a public client, wrong for most")
		}
		if c.trustEmail {
			warn("CODEDISTILL_OIDC_TRUST_EMAIL=1: emails count even when the provider does not say it verified them")
		}
	case c.mode == "proxy":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := api.NewProxyAuth(ctx, c.proxy); err != nil {
			bad("%v", err)
		} else {
			ok("signed identity from %s", c.proxy.Kind)
			if u := proxyKeysURL(c.proxy); u != "" {
				if err := reachable(ctx, u); err != nil {
					bad("cannot reach the proxy's signing keys at %s: %v", u, err)
				} else {
					ok("signing keys reachable at %s", u)
				}
			}
		}
		if c.proxy.LogoutURL == "" {
			warn("CODEDISTILL_PROXY_LOGOUT_URL is not set: Sign out will not end the proxy's session")
		}
	}
	if c.mode != "" {
		if c.adminEmail == "" {
			bad("CODEDISTILL_ADMIN_EMAIL is not set: nobody can become the first owner")
		} else {
			ok("first owner: %s (becomes owner on signing in, while the server has none)", c.adminEmail)
		}
		if len(c.domains) > 0 {
			ok("only these email domains may sign in: %s", strings.Join(c.domains, ", "))
		} else {
			warn("CODEDISTILL_ALLOWED_DOMAINS is not set: anyone the provider signs in may join, while seats last")
		}
	}

	if *dbPath != "" {
		fmt.Println("Server")
		store, err := sqlite.Open(*dbPath)
		if err != nil {
			bad("open %s: %v", *dbPath, err)
		} else {
			defer store.Close()
			ctx := context.Background()
			if n, err := store.CountActiveOwners(ctx, "local"); err != nil {
				bad("count owners: %v", err)
			} else if n == 0 {
				warn("no active owner yet: the first owner becomes one on signing in")
			} else {
				ok("%d active owner%s", n, map[bool]string{true: "", false: "s"}[n == 1])
			}
			if n, err := store.CountSeatsTaken(ctx, "local"); err == nil {
				ok("%d license seat%s taken", n, map[bool]string{true: "", false: "s"}[n == 1])
			}
		}
	}
	if problems > 0 {
		return fmt.Errorf("%d problem%s: fix %s before anyone signs in", problems,
			map[bool]string{true: "", false: "s"}[problems == 1], map[bool]string{true: "it", false: "them"}[problems == 1])
	}
	fmt.Println("Sign-in is ready.")
	return nil
}

// proxyKeysURL is where a proxy's signing keys are published, for the check.
func proxyKeysURL(p api.ProxyConfig) string {
	switch p.Kind {
	case api.ProxyGoogleIAP:
		return "https://www.gstatic.com/iap/verify/public_key-jwk"
	case api.ProxyCloudflareAccess:
		team := strings.TrimPrefix(strings.TrimPrefix(p.Team, "https://"), "http://")
		if !strings.Contains(team, ".") {
			team += ".cloudflareaccess.com"
		}
		return "https://" + strings.TrimRight(team, "/") + "/cdn-cgi/access/certs"
	}
	return "" // AWS publishes one key per id; there is no list to fetch
}

func reachable(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}
