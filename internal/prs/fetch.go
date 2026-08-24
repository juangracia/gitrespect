package prs

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Fetch collects merge request activity for one window and shapes it into a
// Result. It is the single entry point the CLI uses.
func Fetch(ctx context.Context, opts Options) (Result, error) {
	if err := opts.validate(); err != nil {
		return Result{}, err
	}
	// Validate the identity flags before spending a single API call on a
	// query whose results could not be attributed anyway.
	if _, err := newMatcherForOptions(opts); err != nil {
		return Result{}, err
	}

	provider, err := NewProvider(opts)
	if err != nil {
		return Result{}, err
	}
	fetched, err := provider.Fetch(ctx, opts.Since, opts.Until)
	if err != nil {
		return Result{}, err
	}
	return Aggregate(fetched, opts)
}

// FetchComparison collects two windows through a single provider and returns
// the before/after multiplier for merge request volume, the same shape the
// lines of code comparison produces. Labels are free text, typically the
// period strings the user typed.
func FetchComparison(ctx context.Context, opts Options, before, after Window) (Comparison, error) {
	beforeOpts, afterOpts := opts, opts
	beforeOpts.Since, beforeOpts.Until = before.Since, before.Until
	afterOpts.Since, afterOpts.Until = after.Since, after.Until

	if err := beforeOpts.validate(); err != nil {
		return Comparison{}, fmt.Errorf("before period: %w", err)
	}
	if err := afterOpts.validate(); err != nil {
		return Comparison{}, fmt.Errorf("after period: %w", err)
	}
	if _, err := newMatcherForOptions(opts); err != nil {
		return Comparison{}, err
	}

	provider, err := NewProvider(opts)
	if err != nil {
		return Comparison{}, err
	}

	beforeRes, err := fetchWith(ctx, provider, beforeOpts)
	if err != nil {
		return Comparison{}, fmt.Errorf("before period: %w", err)
	}
	afterRes, err := fetchWith(ctx, provider, afterOpts)
	if err != nil {
		return Comparison{}, fmt.Errorf("after period: %w", err)
	}

	c := CompareResults(beforeRes, afterRes)
	c.BeforeLabel, c.AfterLabel = before.Label, after.Label
	return c, nil
}

// newMatcherForOptions builds the matcher the same way Aggregate will, so a
// bad --map or an ambiguous identity fails before any API call is spent.
func newMatcherForOptions(opts Options) (*Matcher, error) {
	identities, _ := opts.identities()
	return NewMatcherFor(identities, opts.Mappings)
}

// Window is one labelled reporting period.
type Window struct {
	Label string
	Since time.Time
	Until time.Time
}

func fetchWith(ctx context.Context, p Provider, opts Options) (Result, error) {
	fetched, err := p.Fetch(ctx, opts.Since, opts.Until)
	if err != nil {
		return Result{}, err
	}
	return Aggregate(fetched, opts)
}

// NewProvider wires a provider to whichever authentication path is available.
func NewProvider(opts Options) (Provider, error) {
	tr, err := newTransport(opts)
	if err != nil {
		return nil, err
	}
	switch opts.Provider {
	case ProviderGitHub:
		return newGitHubProvider(opts.Scope, tr), nil
	default:
		return newGitLabProvider(opts.Scope, tr), nil
	}
}

// newTransport picks the authentication path, in the order that asks least of
// the user: an explicit token, then the provider's environment variable, then
// the locally authenticated CLI, which needs no token handling at all.
func newTransport(opts Options) (transport, error) {
	token := opts.Token
	if token == "" {
		for _, key := range tokenEnvKeys(opts.Provider) {
			if v := opts.getenv(key); v != "" {
				token = v
				break
			}
		}
	}
	if token != "" {
		base := gitlabBaseURL(opts.BaseURL)
		if opts.Provider == ProviderGitHub {
			base = githubBaseURL(opts.BaseURL)
		}
		// validate() already rejected an unusable --api-url; this is the one
		// shape that is allowed but worth saying out loud, once, before the
		// token goes anywhere.
		if plaintext, _ := checkAPIURL(opts.BaseURL); plaintext {
			opts.warn("%s", plaintextWarning(opts.Provider, base))
		}
		return &httpTransport{
			provider: opts.Provider,
			base:     base,
			token:    token,
			client:   httpClient(opts.HTTPClient),
		}, nil
	}

	bin := cliBinary(opts.Provider)
	look := opts.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look(bin); err != nil {
		return nil, fmt.Errorf(
			"no credentials for %s: set %s (or pass --token), or install and authenticate the %s CLI (%s auth login) so gitrespect can reuse it",
			opts.Provider, tokenEnvName(opts.Provider), bin, bin)
	}
	runner := opts.Runner
	if runner == nil {
		runner = execRunner{}
	}
	return &cliTransport{provider: opts.Provider, bin: bin, runner: runner}, nil
}

// tokenEnvKeys lists the environment variables checked for a token, most
// specific first. GH_TOKEN is included because that is what gh itself uses, so
// it is usually already set on a machine that has gh configured.
func tokenEnvKeys(provider string) []string {
	if provider == ProviderGitHub {
		return []string{"GITHUB_TOKEN", "GH_TOKEN"}
	}
	return []string{"GITLAB_TOKEN"}
}
