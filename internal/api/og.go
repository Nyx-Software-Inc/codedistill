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

package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// OGMetadata is the subset of OpenGraph fields the rich-canvas Slice
// 3 fetcher extracts. ImageURL is the absolute (post-redirect-resolved)
// URL of the og:image; the orchestrator (fetchAndStoreOG) is
// responsible for fetching it and stuffing the bytes into the
// BlobStore.
type OGMetadata struct {
	Title       string
	Description string
	ImageURL    string
}

// ogFetchTimeout caps both the HTTP fetch + the parser walk. Keep
// it tight — link items render whether or not OG data lands, and a
// slow server shouldn't tie up a worker goroutine. 5s matches the
// design doc.
const ogFetchTimeout = 5 * time.Second

// ogMaxBodyBytes caps how much HTML we'll buffer + parse. OpenGraph
// meta tags live in <head>, which on real sites tops out around
// 50-100KB even with heavy SEO. 1MB is plenty and prevents a
// malicious server from streaming gigabytes.
const ogMaxBodyBytes = 1 << 20 // 1 MiB

// ogMaxRedirects bounds the redirect chain. 3 hops covers normal
// patterns (http→https, apex→www, shortener→canonical) without
// inviting loops.
const ogMaxRedirects = 3

// fetchOG retrieves the URL with conservative timeouts + caps and
// returns the parsed OpenGraph metadata. Never returns nil on
// non-error paths; an empty OGMetadata means "fetched, but no OG
// tags found." That's a valid outcome — the caller should still
// stamp og_fetched_at so we don't re-fetch a site that just doesn't
// publish OG data.
//
// Errors are returned only for transport/protocol failures
// (timeout, DNS, bad URL, non-2xx, non-HTML Content-Type). The
// caller logs + stamps og_fetched_at on error too so the same
// "we tried" semantics apply.
func fetchOG(ctx context.Context, rawURL string) (*OGMetadata, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	ctx, cancel := context.WithTimeout(ctx, ogFetchTimeout)
	defer cancel()

	client := &http.Client{
		// CheckRedirect bounds the chain. Returning ErrUseLastResponse
		// here would let us see the redirect headers; we let the
		// default behavior (follow, transparently) handle it but cap
		// the count.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= ogMaxRedirects {
				return fmt.Errorf("too many redirects (>%d)", ogMaxRedirects)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// Identify ourselves politely. Some sites serve different markup
	// (or 403) to anonymous UAs; this gets us closer to "a real
	// browser" without lying about it.
	req.Header.Set("User-Agent", "CodeDistill/og-fetcher")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "text/html") &&
		!strings.HasPrefix(strings.ToLower(ct), "application/xhtml") {
		return nil, fmt.Errorf("unexpected content-type %q", ct)
	}

	body := io.LimitReader(resp.Body, ogMaxBodyBytes)
	og := parseOG(body)
	// Resolve relative og:image URLs against the final URL after
	// redirects. resp.Request.URL is the post-redirect URL the
	// stdlib remembered.
	if og.ImageURL != "" {
		if img, err := url.Parse(og.ImageURL); err == nil {
			if !img.IsAbs() {
				og.ImageURL = resp.Request.URL.ResolveReference(img).String()
			}
		}
	}
	return og, nil
}

// parseOG walks the HTML stream looking for <meta property="og:*">
// tags. Stops at </head> (OG tags live in head; reading past that
// is wasted IO on what's typically the largest part of a page).
// Returns an empty OGMetadata if no OG tags are found — never nil.
//
// Tolerance notes:
//   - Both `property="og:foo"` and `name="og:foo"` are accepted.
//     Spec calls for property=; some CMSes emit name= and we'd
//     rather not miss them.
//   - Self-closing variants (`<meta ... />`) and HTML5 variants
//     (`<meta ...>`) both parse identically through html.Tokenizer.
//   - Attribute order doesn't matter; we collect all attrs into a
//     map first, then pick by key.
func parseOG(r io.Reader) *OGMetadata {
	out := &OGMetadata{}
	tk := html.NewTokenizer(r)
	for {
		tt := tk.Next()
		switch tt {
		case html.ErrorToken:
			return out
		case html.EndTagToken:
			name, _ := tk.TagName()
			if string(name) == "head" {
				return out
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tk.TagName()
			if string(name) != "meta" || !hasAttr {
				continue
			}
			attrs := map[string]string{}
			for {
				k, v, more := tk.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
				if !more {
					break
				}
			}
			key := attrs["property"]
			if key == "" {
				key = attrs["name"]
			}
			content := attrs["content"]
			if content == "" {
				continue
			}
			switch strings.ToLower(key) {
			case "og:title":
				if out.Title == "" {
					out.Title = content
				}
			case "og:description":
				if out.Description == "" {
					out.Description = content
				}
			case "og:image":
				if out.ImageURL == "" {
					out.ImageURL = content
				}
			}
		}
	}
}

// ogImageMaxBytes caps the og:image download. Generous because hero
// images on real sites are often 200-800KB; 5MB is plenty without
// inviting abuse.
const ogImageMaxBytes = 5 << 20 // 5 MiB

// fetchAndStoreOG runs the full pipeline for a freshly-created link
// item: fetch OG metadata from the link, optionally fetch + store
// the og:image as a blob via the BlobStore, then update the
// scratchpad_item row with the results. ALWAYS stamps og_fetched_at
// on completion (success or failure) so we don't re-fetch
// indefinitely if a site has no OG data or won't respond.
//
// Intended to run in a goroutine spawned from createItem. Uses
// whatever context the caller hands in — request contexts die when
// the response is sent, so callers should pass context.Background()
// or a longer-lived ctx tied to the server's shutdown.
//
// Safe to call with blobs=nil — og_title and og_description still
// land; og_image_sha just stays empty.
func (s *Server) fetchAndStoreOG(ctx context.Context, itemID, linkURL string) {
	item, err := s.store.GetScratchpadItem(ctx, itemID)
	if err != nil {
		s.log.Warn("og fetch: item gone before fetch", "id", itemID, "err", err)
		return
	}

	og, fetchErr := fetchOG(ctx, linkURL)
	now := time.Now().UTC()
	if fetchErr != nil {
		s.log.Debug("og fetch failed", "id", itemID, "url", linkURL, "err", fetchErr)
		// Stamp og_fetched_at on failure too so we don't retry
		// indefinitely. Empty title/description/image with non-nil
		// fetched_at means "we tried, no metadata available."
		item.OGFetchedAt = &now
		item.UpdatedAt = now
		if err := s.store.UpdateOGMetadata(ctx, item); err != nil {
			s.log.Warn("og fetch: stamp on failure failed", "id", itemID, "err", err)
		}
		return
	}

	item.OGTitle = og.Title
	item.OGDescription = og.Description
	if og.ImageURL != "" && s.blobs != nil {
		if sha, err := fetchAndStoreOGImage(ctx, s.blobs, og.ImageURL); err == nil {
			item.OGImageSHA = sha
		} else {
			s.log.Debug("og:image fetch failed", "id", itemID, "url", og.ImageURL, "err", err)
		}
	}
	item.OGFetchedAt = &now
	item.UpdatedAt = now
	if err := s.store.UpdateOGMetadata(ctx, item); err != nil {
		s.log.Warn("og fetch: update failed", "id", itemID, "err", err)
	}
}

// fetchAndStoreOGImage downloads the image at imgURL (with the same
// timeout / size caps as fetchOG) and stuffs the bytes into the
// BlobStore. Returns the resulting SHA. Reuses the BlobStore's
// dedup so two link items pointing at the same og:image share
// storage.
func fetchAndStoreOGImage(ctx context.Context, blobs interface {
	// Inline the subset of blobstore.BlobStore we need. Avoids
	// importing internal/blobstore directly into this file beyond
	// what the surrounding package already does.
	Put(ctx context.Context, r io.Reader) (sha string, size int64, err error)
}, imgURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ogFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CodeDistill/og-fetcher")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body := io.LimitReader(resp.Body, ogImageMaxBytes)
	sha, _, err := blobs.Put(ctx, body)
	return sha, err
}
