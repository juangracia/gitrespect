package prs

import (
	"fmt"
	"net/url"
	"strings"
)

// checkAPIURL validates an --api-url override and reports whether it is
// plaintext.
//
// Three shapes used to fail in three unhelpful ways: a schemeless host silently
// produced "gitlab.corp.com/api/v4" and failed later with a confusing error, a
// non-http scheme like file:// was appended to rather than rejected, and plain
// http shipped the token across the network in cleartext without a word. A
// wrong API root is also the most plausible route back into a redirect that
// carries credentials, so it is worth refusing early rather than documenting.
func checkAPIURL(raw string) (plaintext bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// The built-in defaults are https.
		return false, nil
	}

	u, parseErr := url.Parse(raw)
	if parseErr != nil {
		return false, fmt.Errorf("invalid --api-url %q: %w", raw, parseErr)
	}

	switch u.Scheme {
	case "https":
	case "http":
		plaintext = true
	case "":
		return false, fmt.Errorf(
			"invalid --api-url %q: missing scheme, expected https://host (or http://host for a plaintext instance)", raw)
	default:
		return false, fmt.Errorf(
			"invalid --api-url %q: scheme %q is not supported, expected https:// or http://", raw, u.Scheme)
	}

	if u.Host == "" {
		return false, fmt.Errorf("invalid --api-url %q: no host, expected https://host", raw)
	}
	// Credentials in the URL would be sent to whatever host follows the '@',
	// which is rarely the host the user thinks they typed. gitrespect has its
	// own token handling, so there is no reason to accept them here.
	if u.User != nil {
		return false, fmt.Errorf(
			"invalid --api-url %q: remove the credentials before the '@'; use --token or %s instead",
			redactUserinfo(u), tokenEnvName(ProviderGitLab))
	}
	if hasDotSegment(u.Path) {
		return false, fmt.Errorf(
			"invalid --api-url %q: path segments \".\" and \"..\" are not allowed", raw)
	}
	return plaintext, nil
}

// redactUserinfo renders a URL for an error message without echoing whatever
// secret was sitting in its userinfo.
func redactUserinfo(u *url.URL) string {
	clone := *u
	clone.User = url.User("...")
	return clone.String()
}

// checkScope rejects a group or org whose path segments would resolve to a
// different endpoint than the one asked for.
//
// A bare ".." escapes correctly as a segment but still produces
// /groups/../merge_requests, which a server normalizes into a valid but
// different route. That returns a confident answer about a scope the user
// never requested, which is worse than an error.
func checkScope(provider, scope string) error {
	scope = strings.TrimSpace(scope)
	if hasDotSegment(scope) {
		flag := "--group"
		if provider == ProviderGitHub {
			flag = "--org"
		}
		return fmt.Errorf("invalid %s %q: path segments \".\" and \"..\" are not allowed", flag, scope)
	}
	return nil
}

// hasDotSegment reports whether any slash-separated segment is "." or "..".
func hasDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

// plaintextWarning is what the user sees once when their API root is http.
// It names both ways out, so it is actionable rather than a nag.
func plaintextWarning(provider, base string) string {
	return fmt.Sprintf(
		"warning: --api-url %s is plaintext http, so the %s token crosses the network unencrypted; use https, or drop --token/%s and let %s handle auth\n",
		base, provider, tokenEnvName(provider), cliBinary(provider))
}
