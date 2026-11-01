package pihole

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FTL restarts itself to apply some configuration changes, and it does so
// after it has answered the request that made the change. The restart begins
// a moment later and leaves the API unreachable for a couple of seconds. A
// request sent in that window can be applied and then lose its reply, so a
// caller that went straight on to its next write saw an EOF for a change that
// had in fact been made, and a retry then failed with "already present". The
// helpers below let a write wait until FTL is back before reporting.
//
// FTL restarts only when a changed setting carries FLAG_RESTART_FTL (or the
// privacy level was lowered, or a backup was imported), so waiting after every
// write would stall the many writes that never restart anything. RestartsFTL
// asks FTL which settings carry the flag rather than keeping a list here that
// would drift from FTL's own.

const (
	// restartWaitTimeout bounds the wait for FTL to come back. A restart takes
	// a couple of seconds on a Raspberry Pi; this leaves room for a slow one.
	restartWaitTimeout = 30 * time.Second

	// restartPollInterval is the gap between uptime checks while waiting.
	restartPollInterval = 250 * time.Millisecond

	// restartProbeTimeout bounds one uptime check, so a probe sent into the
	// shutdown window does not hold the wait up.
	restartProbeTimeout = 2 * time.Second
)

// WithRestartWait sets how long a write waits for FTL to come back after a
// restart it caused. Mainly for tests; the default suits real hardware.
func WithRestartWait(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.restartWait = d
		}
	}
}

// RestartsFTL reports whether a change to the dotted config element makes FTL
// restart, from the flags FTL publishes for it at
// GET /config/<element>?detailed=true.
func (c *Client) RestartsFTL(ctx context.Context, element string) (bool, error) {
	parts := strings.Split(element, ".")
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = EscapePathSegment(p)
	}

	var resp ConfigResponse
	if err := c.Get(ctx, "/config/"+strings.Join(escaped, "/")+"?detailed=true", &resp); err != nil {
		return false, err
	}
	node, ok := DetailedConfigItem(resp.Config, element)
	if !ok {
		return false, fmt.Errorf("pi-hole has no config item %q", element)
	}
	return node.RestartsFTL, nil
}

// DetailedItem is one setting from a detailed config read.
type DetailedItem struct {
	Value       any
	RestartsFTL bool
	// EndsSessions is set for a setting whose change logs every API session
	// out, this server's included, such as the admin password.
	EndsSessions bool
}

// DetailedConfigItem finds the dotted element in a config tree read with
// ?detailed=true, where each setting is an object carrying "value" and
// "flags" in place of its bare value.
func DetailedConfigItem(cfg map[string]any, element string) (DetailedItem, bool) {
	var node any = cfg
	for _, part := range strings.Split(element, ".") {
		m, ok := node.(map[string]any)
		if !ok {
			return DetailedItem{}, false
		}
		node, ok = m[part]
		if !ok {
			return DetailedItem{}, false
		}
	}
	leaf, ok := node.(map[string]any)
	if !ok {
		return DetailedItem{}, false
	}
	value, isSetting := leaf["value"]
	if !isSetting {
		return DetailedItem{}, false
	}
	flags, _ := leaf["flags"].(map[string]any)
	restarts, _ := flags["restart_dnsmasq"].(bool)
	endsSessions, _ := flags["session_reset"].(bool)
	return DetailedItem{Value: value, RestartsFTL: restarts, EndsSessions: endsSessions}, true
}

// AwaitRestart waits until FTL has restarted after a request sent at sent.
//
// It polls /info/ftl until a reply reports an uptime shorter than the time
// since sent, which only a process started after the request can do. The old
// process keeps answering for a moment before it stops, and nothing answers
// while the new one starts, so both are polled through. It gives up after
// restartWaitTimeout, or when ctx ends.
func (c *Client) AwaitRestart(ctx context.Context, sent time.Time) error {
	wait := c.restartWait
	if wait <= 0 {
		wait = restartWaitTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	tick := time.NewTicker(restartPollInterval)
	defer tick.Stop()
	for {
		if up, err := c.ftlUptime(ctx); err == nil && up < time.Since(sent) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("pi-hole had not finished restarting %s after the change: %w",
				time.Since(sent).Round(time.Second), ctx.Err())
		case <-tick.C:
		}
	}
}

// ftlUptime reads how long the running FTL process has been up. It makes one
// attempt with no retries, since the caller polls.
func (c *Client) ftlUptime(ctx context.Context) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, restartProbeTimeout)
	defer cancel()

	sid, err := c.ensureAuth(ctx)
	if err != nil {
		return 0, err
	}
	var info FTLInfo
	err = c.doRequest(ctx, http.MethodGet, "/info/ftl", sid, nil, &info)
	var authErr *AuthError
	if errors.As(err, &authErr) {
		// A restarted FTL may not have kept this session.
		if sid, err = c.handleAuthRetry(ctx, sid); err != nil {
			return 0, err
		}
		err = c.doRequest(ctx, http.MethodGet, "/info/ftl", sid, nil, &info)
	}
	if err != nil {
		return 0, err
	}
	// FTL reports its uptime in milliseconds (timer_elapsed_msec in
	// src/api/info.c, v6.7 and v6.8).
	return time.Duration(info.FTL.Uptime * float64(time.Millisecond)), nil
}
