# Changelog

All notable changes to `pihole-mcp` are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release body on GitHub for each tagged version is sourced from the matching section in this file. The `[Unreleased]` section accumulates user-visible changes between releases.

## [Unreleased]

### Security

- **The published binaries were built with a Go toolchain carrying six reachable standard-library advisories.** `go.mod` pinned `toolchain go1.26.5`, and because `actions/setup-go` resolves the toolchain line rather than the `go` line, that pin is what compiled every downloadable artefact and container image. Scanning the module with that toolchain selected reports GO-2026-5026, GO-2026-5972, GO-2026-6089, GO-2026-6090, GO-2026-6091 and GO-2026-6218 as reachable from this code, all of them fixed upstream in go1.26.6. The pin has been raised to `go1.26.7`, against which the same scan reports no vulnerabilities. Present since v0.6.0 (12 July 2026) and shipped in every release from v0.6.0 through v0.8.1.
- **The govulncheck merge gate was scanning a different Go release from the one being shipped, and reported clean throughout.** `golang/govulncheck-action` declares a `go-version-input` with a hard default of `stable` and forwards it to `actions/setup-go` unconditionally. `setup-go` returns on the first non-empty `go-version`, so that default silently outranked the `go-version-file: go.mod` this workflow was passing, and the job analysed whatever Go release was newest at the time instead of the pinned toolchain. The advisories above were therefore invisible to the only check that existed to catch them. The input is now pinned to an explicit empty string so the file wins and the gate follows `go.mod`.
- **The HTTP and SSE transports accepted every request that reached them.** Neither had any authentication. The README presented Origin and Host validation as the security model, but that is DNS-rebinding protection for browsers: both headers are chosen by the caller, so any client that is not a browser sets them to whatever the allowlist accepts and is through. Anyone who could reach the port could disable blocking, rewrite blocklists and read the query history. An optional shared bearer token is now available through `PIHOLE_HTTP_AUTH_TOKEN`, or `PIHOLE_HTTP_AUTH_TOKEN_FILE` to keep it out of `ps`; requests without it are refused with 401 before the rate limiter or any tool runs, the token is compared in constant time, and rejected requests are charged to a failed-authentication budget kept separate from the request limit, so the token cannot be guessed at line rate and a flood of wrong tokens cannot lock out a client at the same address that holds it. Binding an address that is not loopback with no token now logs an error at startup and starts anyway, so a reverse proxy holding the authentication stays a valid deployment. The README section that implied otherwise has been rewritten. Present since v0.4.0 (23 May 2026), when the transports were first hardened.
- **The rate limiter could be switched off by any caller willing to vary one header.** Buckets were keyed on `Mcp-Session-Id` first, which is a header the client writes with no server-side proof, so a caller sending a fresh identifier on every request got a fresh full bucket every time and was never throttled. The same key was also unbounded, so those requests grew the bucket map for as long as they kept arriving. The fallback for requests carrying no session id was no better: it keyed on `RemoteAddr`, which includes the ephemeral port and therefore identifies a connection rather than a client. Requests are now charged first to a ceiling per client address, with the session as a secondary bucket beneath it, and the number of session buckets one address may create is capped. `PIHOLE_RATE_LIMIT` remains the per-session rate; one address may send four times it. Behind a reverse proxy, name the proxy networks in the new `PIHOLE_TRUSTED_PROXIES` and `X-Forwarded-For` is used to tell clients apart; nothing is trusted by default, because that header is client-supplied too. This also matters for what comes next: MCP revision `2026-07-28` removes `Mcp-Session-Id` from the transport altogether, and under the old keying every request would have fallen to the per-connection fallback and the limiter would have stopped limiting silently. Present since v0.4.0 (23 May 2026).

### Changed

- **`tools/list` is substantially smaller with more than one Pi-hole configured.** Two things were repeated into every tool. The optional `instance` parameter carried a prose description naming every configured instance, restated on all 76 tools that accept it, and the aggregate output schema was copied whole into each of the seven tools that declare one. The instance names are now an enum, which is what a client needs to offer a valid choice and a fraction of the size, and the readable enumeration lives once each in the server instructions and the `pihole://instances` resource. The aggregate schema branch is trimmed to the fields a client reads. Multi-instance mode now adds about 35% to the listing rather than about 56%.
- **The server instructions no longer claim every tool accepts `detail` and `format`.** 15 tools accept `detail` and 20 accept `format`, out of 84, and that sentence was injected into every conversation as an unqualified statement. It now says "some tools".
- **The `pihole_config_*` descriptions name `dns.hosts` and `dns.cnameRecords`** alongside `dns.upstreams`, and point at the dedicated local DNS tools as the better route to either.
- **Tool results are now validated against the output schemas they declare.** The schemas are generated from the Go types the handlers return, so a result that contradicts one is a genuine defect rather than an approximate schema, and it fails silently: the client deserialises into the declared shape and reads zero values. Such a result is now refused rather than returned. Input validation is deliberately not enabled alongside it, because the SDK currently accepts a numeric argument sent as a string, which is common from model clients, and rejecting those would break calls that work today.
- **`docs/TOOLS.md` now shows the numeric bounds each parameter declares.** The generated reference rendered enum options but not `minimum` or `maximum`, so a bound the schema enforces was visible only as prose in the description, indistinguishable from advice. Bounded parameters now read as `number (1 to 50)` beside the type.
- **`pihole_history_database` and `pihole_stats_database` now say what separates them.** Both read the long-term database over a date range, and their descriptions were close enough that the choice between them was arbitrary. One returns a time series with a bucket per slot, the other a single set of totals for the whole range, and each description now says so and points at the other.
- **`pihole_stats_recent_blocked` caps `count` at 50**, matching the other ranked statistics tools. It previously passed the value straight through.
- **Dependabot now watches the Pi-hole image pinned in the Compose files.** `docker-compose.dev.yml` and `.github/docker-compose.ci.yml` pin a `pihole/pihole` tag, but Dependabot's `docker` ecosystem reads Dockerfiles only and rejects YAML, so that pin drifted with nothing tracking it while local development and the integration job tested against a Pi-hole release nobody had chosen. A `docker-compose` ecosystem now covers both directories. Base-image updates are grouped so the distroless digest, which appears in both `Dockerfile` and `Dockerfile.goreleaser` and must match, can no longer land in one file and not the other. Action pins move from a weekly to a monthly cadence.
- **`pihole_domains_add`, `pihole_groups_add`, `pihole_clients_add`, `pihole_lists_add` and `pihole_config_add_value` no longer carry a destructive hint.** Each only ever creates a new row or appends a config value, and mcp-go's tool builder defaults `destructiveHint` to true unless a tool overrides it, so all five were flagged the same as the update and delete tools that overwrite or remove one. A client picking which write to confirm before running had no way to tell an addition from an overwrite.
- **`pihole_domains_update` and `pihole_lists_update` no longer promise a capability they do not have.** `pihole_domains_update` said it could "move" a domain between the allow and deny lists; changing `type` or `kind` creates a duplicate entry rather than moving the original, verified against a live Pi-hole. `pihole_lists_update` said it could change group assignments; it has no parameter for one. Both descriptions now say only what the tool does.

### Added

- **Read-only mode, so a Pi-hole can be inspected without granting the ability to change it.** The server has always offered its whole surface, and 33 of the 82 tools write, 27 of them destructively: someone who only wanted to ask questions about their Pi-hole had to hand over `pihole_teleporter_import`, four batch deletes and `pihole_action_flush_logs` to do it. Setting `PIHOLE_READ_ONLY=true` exposes only the 49 tools that cannot change anything. Write tools are absent from `tools/list` and rejected if called anyway, so a client working from a cached listing, or a model producing a tool name from memory, cannot reach one either. The instructions the server sends are rewritten to match, rather than continuing to recommend tools that are no longer there.
- **`PIHOLE_TOOLSETS` selects which families of tools exist.** Eighteen published names, listed with their contents in [docs/TOOLS.md](docs/TOOLS.md#toolsets), so `PIHOLE_TOOLSETS=dashboard,domains,lists` offers the 12 tools blocklist curation needs and nothing else. This is worth doing on its own: `tools/list` is a cost paid in every conversation, and that selection is roughly a fifth of the full payload. Unset, or the explicit `all`, keeps every tool, so an existing deployment needs no action. Read-only and toolsets intersect, and read-only always wins. An unknown name fails at startup with every valid name in the message, and `pihole-mcp -check` now reports what the scoping resolved to and how many tools survived it.
- **Six tools for local DNS and CNAME records**, the last thing this server could not do that a Pi-hole owner regularly wants. `pihole_local_dns_list`, `_add` and `_delete` manage the A and AAAA records in `dns.hosts`, and the matching `pihole_local_cname_*` trio manage `dns.cnameRecords`. Callers pass an IP address and a hostname, or an alias and a target, rather than assembling the space-separated and comma-separated strings Pi-hole stores; the record type follows from the address family. Deleting reads the current records and removes the exact stored entry rather than a reconstruction of it, so a host record carrying several names for one address is matched rather than missed. Doing this through `pihole_config_add_value` was possible before but required knowing the encoding, and the space in a host record is precisely the value the old path handling corrupted.
- **A `-check` flag that tells you whether the server can actually reach your Pi-hole.** It loads the configuration, contacts every configured instance once, and prints PASS or FAIL per instance with the resolved URL and the Pi-hole and FTL versions it found, exiting non-zero if any fails. Almost every report of an MCP server "not working" is a configuration or reachability problem, and the client that launched the server usually discards its output, so a wrong URL or password looks identical to a server that never started. The README now points at it from the installation section and from every client setup block.
- **Connection failures now come with the fix rather than the raw Go error.** An untrusted HTTPS certificate, a refused connection, a hostname that does not resolve and a URL missing its scheme each reached the caller as text such as `dial tcp 192.168.1.2:80: connect: connection refused`. The README documents what to do about all four, but the model calling the tool never reads the README, so its only option was to retry. Each now carries the same guidance the README gives, and `-check` reports it too so the two cannot drift apart.
- **The server now advertises its display name, description, website and icons over the protocol.** A client had only the bare package name `pihole-mcp` and its version to show a user, so the server appeared in client interfaces without a readable name or a logo. All four fields are sourced from the same values as the MCP Registry manifest, with a test asserting the two agree, so the listing someone installs from and the running server can no longer describe different things. The icon set now leads with PNG, since the specification tells clients to prefer PNG or JPEG and warns that an SVG may carry executable content.
- **Every tool now carries its display title in the field current clients read.** The titles have always been there, but only as an annotation, which is where the field lived before the 2025-11-25 revision moved it onto the tool itself. A client written against the current specification read the empty field and fell back to the raw name, so a tool picker showed `pihole_stats_summary` where it could have shown "Query Statistics". Both fields are now set.
- **A binary installed with `go install` reports its real version instead of `dev`.** The version is injected at build time by the release pipeline, so anyone installing straight from the module path got a binary that identified itself as `dev` in `-version` and in every handshake, which is exactly the group least able to say which build a bug report is about. The module version recorded in the build info is now used whenever the build flag is absent.
- **`pihole_queries_search` can now reach the long-term database.** A new `disk` parameter forwards Pi-hole's own flag, so a search over a historical range reads the on-disk query database rather than FTL's short in-memory window.
- **The four history tools accept `detail` and `format`.** At `detail=full` or `format=csv` they return one row per time slot instead of a summary of how many slots there were. A full day is roughly 145 rows, so it is opt-in rather than the default.
- **The five list tools accept `limit` and `offset`.** `pihole_domains_list`, `pihole_clients_list`, `pihole_lists_list`, `pihole_groups_list` and `pihole_dhcp_leases` can now be paged. Pi-hole returns these collections whole, and a gravity-backed denylist runs to thousands of entries, so previously the only available answer was all of them.
- **`pihole_info_database` reports how far back the stored queries reach**, both in the database as a whole and on disk. That figure decides whether a historical query search can succeed at all, so it is worth knowing before the search rather than after it comes back empty.
- **`pihole_config_set` accepts a `restart` parameter**, matching its two sibling configuration-array tools. Every write previously restarted FTL's DNS resolver immediately, so chaining several `config_set` calls to build up a configuration cost one DNS interruption per call rather than one at the end.
- **`pihole_teleporter_export` accepts an `output_path` parameter**, and its description now says plainly that the exported file persists on disk and is the caller's to move or delete. Under the documented Docker install the file was written inside a container that `--rm` deletes on exit with nothing in the tool's own output to warn of it; the README's Docker block now documents a volume mount to keep the backup on the host.

### Fixed

- **A panic in a resource handler killed the whole server.** Panic recovery was installed for the tool path only, so a panic while reading a resource escaped the message loop and took the process down with it. On stdio, which is the default transport and how nearly every user runs this, the client saw its connection drop with no error and nothing to explain it. Resource handlers are now recovered the same way tool handlers are, and answer with an internal error instead. Present since v0.1.0 (6 April 2026).
- **The advertised five second shutdown grace period was applied to nothing.** On SIGTERM the HTTP and SSE transports started a graceful shutdown in the background and then returned immediately, because the listener closes at the start of that shutdown rather than the end. The process exited around 25 milliseconds later, cutting off any client mid-message. Shutdown also never closed the transport's own sessions, and the long-lived request both MCP HTTP transports use to push messages to a client does not end on its own, so had anything waited it would have burned the entire grace period and then killed the connection anyway. Sessions are now closed first, re-swept on a ticker so one opened during the shutdown cannot hold it up, and the process waits for the drain to finish before exiting. Present since v0.4.0 (23 May 2026).
- **A `PIHOLE_URL` without a scheme started the server cleanly and failed every tool call afterwards.** `192.168.1.2` and `pihole.local` parse as valid URLs, so nothing rejected them, and the failure surfaced later as `unsupported protocol scheme ""` from deep inside the HTTP client, naming neither the variable at fault nor the fix. The URL is now checked when the configuration loads, and a scheme that is not http or https is refused with a message that names the variable and shows a correct value. Present since v0.1.0 (6 April 2026).
- **`pihole_queries_suggestions` never listed the domains or client names it advertises.** Its description leads with "known domains", and the handler printed the query types, statuses, replies, DNSSEC states, upstreams and client addresses while silently omitting the two lists a caller most needs before filtering. Pi-hole had sent both all along. Every category is now listed, and each is capped with the total named, because the domain and client lists grow with traffic and are unbounded on a busy Pi-hole. Present since v0.1.0 (6 April 2026).
- **`pihole_history_database_clients` could report clients that do not exist.** Pi-hole names the clients on this endpoint by address but keys its per-slot buckets by database row id, so reading the buckets as client identities produced entries called `1` and `2` alongside the real address. The declared clients are now authoritative, and any bucket Pi-hole did not attribute is reported as what it is rather than given a name.
- **`pihole_config_get` could not return a configuration value at the detail level callers get by default.** It counted the keys in each section and printed the counts, so a tool whose description promised the full configuration answered `**dns:** 29 settings` and no setting. Nested sections are now flattened into sorted `dns.cache.size: 10000` lines; `detail=minimal` lists the section names, and `detail=full` is still the raw JSON. Present since v0.1.0 (6 April 2026).
- **`pihole_info_metrics` never returned a metric.** Every figure FTL reports lives one or two levels down, and the renderer summarised each top-level group as `**dns:** 2 sub-keys`, so the tool could not emit a cache hit count, an eviction count or a lease statistic. Metrics are now flattened the same way. Present since v0.2.0 (2 May 2026).
- **The history tools discarded the time series they exist to return.** All four reported only how many data points there were and the totals across them, throwing away every per-slot figure Pi-hole had sent. The series is now reachable through `detail=full` or `format=csv`. Present since v0.1.0 (6 April 2026).
- **`pihole_history_clients` reported no clients on a Pi-hole that had a full day of activity.** FTL can return an empty client map while still bucketing every slot's traffic under a key in the series, and the renderer read only the map. It now derives the missing clients from the series, so the tool answers with the traffic rather than with zero. Present since v0.1.0 (6 April 2026).
- **`pihole_queries_search` silently returned nothing for any historical range.** Pi-hole reads its long-term database only when asked, and the tool never asked, so a search with `from` and `until` set beyond FTL's in-memory window came back empty with no error and no explanation, which is exactly what a Pi-hole with no traffic looks like. The new `disk` parameter reaches the database, and an empty result now says how far back the data goes and how to search further. Present since v0.1.0 (6 April 2026).
- **CSV output was not quoted, so a comment containing a comma silently added a column.** Rows were assembled by joining the fields with commas, which means a comment such as `blocked, permanently`, a quotation mark, or a newline changed the column count of that row alone with nothing in the output to signal it. Fields now go through RFC 4180 quoting, and the output parses back to the values that went in. Present since v0.1.0 (6 April 2026).
- **A negative `count` returned an empty list and reported success.** The value was passed to Pi-hole unchanged, came back with nothing, and rendered as an empty result with no error, which is indistinguishable from a Pi-hole that has no data. `count`, `length`, `limit` and `offset` are now rejected below their floor, naming the parameter and the value, and the bounds are declared on the tool schema. A count above the cap is still capped rather than refused, because asking for more than a tool will give is a request for everything, not a mistake. Present since v0.2.0 (2 May 2026).
- **Identical calls produced different output from one call to the next.** `pihole_config_get` and `pihole_history_clients` rendered straight from a Go map, whose iteration order is deliberately randomised, so the same unchanged Pi-hole answered with its sections and clients in a different order every time and a reader had no way to tell a reordering from a change. Configuration is now sorted by key and clients by query count, busiest first, with ties broken on the client address. Present since v0.1.0 (6 April 2026).
- **`detail=minimal` on `pihole_domains_list` returned as many bytes as `detail=normal`.** The text shrank to a single count while the structured content behind it still carried every entry, so on a gravity-backed list the response asking for less was megabytes either way. Minimal now sends the count with an empty entry list, which still matches the tool's declared output schema. Present since v0.2.0 (2 May 2026).
- **Editing the comment on a disabled domain, list or group silently switched it back on.** Pi-hole treats `PUT` as a full replacement of both the comment and the enabled state, so a request that named only the comment left FTL to default the rest, and its default is enabled. Pausing a blocklist and then annotating why undid the pause, the tool reported "Updated" either way, and nothing in the reply showed which. The update tools now read the entry first and send both fields, so changing one leaves the other as it was. Present since v0.1.0 (6 April 2026).
- **Changing only the enabled state of a domain, list or group erased its comment.** The same full-replacement rule in the other direction: a request carrying only `enabled` left the comment unset, and Pi-hole stored it as empty. Disabling a rule threw away the note explaining why it existed. Both fields are now always sent. Present since v0.1.0 (6 April 2026).
- **`pihole_config_add_value` and `pihole_config_remove_value` silently discarded everything after a `#` in the value.** The value was spliced into the request path unescaped, so Go read everything from the `#` onwards as a URL fragment and dropped it before the request left the process. `127.0.0.1#5335`, the standard Unbound upstream and the most common advanced Pi-hole setup there is, reached Pi-hole as `127.0.0.1`, and the `restart=false` flag was appended after the value so it was swallowed by the same fragment and never applied. Pi-hole answered 200 to the truncated request and the tool reported the full value as written. Present since v0.2.0 (2 May 2026).
- **A blocklist URL carrying a query string, a group whose name contains `#`, and a regex rule containing `+` could all be created and then never updated or removed.** Identifiers went into the request path unescaped. A `?` in a list address turned the rest of the URL into the request's own query string; a `#` in a group name truncated the path at the fragment; and a `+` in a regex rule, which Go's path escaping leaves alone because a plus is legal in a path, is read by Pi-hole as a space. Each reached a different row or none at all, while the reply still said "Updated" or "Deleted". Path segments are now percent-encoded, including the `+` case. Present since v0.1.0 (6 April 2026).
- **Looking up a single group, client or configuration section by name returned a different one, or nothing.** The same unescaped interpolation on the read side: `pihole_groups_list` with `name` set to `Kids #2` asked Pi-hole for the group `Kids `, and `pihole_clients_list` filtered on an identifier such as `eth0#lan` asked for `eth0`. The tool then reported whatever came back as the answer to the question asked. These filters are now encoded like every other path segment. Present since v0.1.0 (6 April 2026).
- **Filter values on `pihole_queries_search` and the other filtered reads were truncated, and could add parameters of their own.** Query strings were assembled by joining raw key and value pairs, so an upstream written `8.8.8.8#53` asked Pi-hole about `8.8.8.8` and took the remaining filters down with it, and a value containing `&` inserted parameters the caller never supplied. Values are now encoded, and the parameters come out in a stable order rather than a different one on every call. Present since v0.1.0 (6 April 2026).
- **Regex denylist rules containing `$` or `|` were rejected before they reached Pi-hole.** Every domain argument was validated as a DNS name, and that check refuses shell metacharacters, so an anchored suffix match such as `.*\.doubleclick\.net$` or an alternation such as `^(ads|track)\.example\.com$` could not be added at all. Validation now follows the `kind` argument: DNS-label rules for an exact entry, and a regular-expression parse for a regex entry, which also catches an unbalanced bracket before it becomes an API error. A pattern using a backreference is passed through to Pi-hole rather than parsed, because Pi-hole's regex engine supports them and Go's does not, and refusing one here would block a rule Pi-hole would have accepted. Present since v0.4.0 (23 May 2026).
- **`pihole_domains_add` rejected every bulk add.** The tool accepts comma-separated domains and validates each one, but sent them to Pi-hole as a single joined string, which Pi-hole answers with `400 Invalid domain`. The rules are now sent as an array. A regex is never split on commas, because a comma is significant inside a quantifier such as `^ads{1,3}\.example\.com`. Present since v0.1.0 (6 April 2026).
- **Updating a client without naming a comment erased the comment it already had.** A comment is the only field a client row has, and Pi-hole replaces it on every write, so calling `pihole_clients_update` to change anything else, or calling it to confirm a value, silently cleared it. The tool now reads the existing comment back and resends it when the caller did not supply one. Present since v0.1.0 (6 April 2026).
- **`pihole_config_set` still applied nothing when the payload arrived wrapped twice.** The guard added in v0.8.1 stripped one `config` envelope, so a payload someone had wrapped by hand on top of one that already carried it reduced to a single wrap and was then wrapped again on the way out. Pi-hole has no top-level `config` section, ignores what it does not recognise, and answers 200, so the write reported success and changed nothing. Every envelope is now stripped. Present since v0.8.1 (30 August 2026).
- **`pihole_clients_batch_delete` documented a payload Pi-hole rejects.** Its example gave a plain array of identifiers, which Pi-hole answers with `400 Batch delete requires an array of objects`, so anyone following the tool's own description failed every time. The three sibling batch-delete tools already documented the correct shape. `pihole_clients_update` also advertised group assignments, which it has no parameter to change; the description now says what the tool actually does.
- **`pihole_instance_sync` and `pihole_instance_diff` saw no local DNS or CNAME records at all.** Pi-hole nests a configuration section under its full path from the root, so a request for `dns.hosts` comes back wrapped rather than flat. The reconciler read the flat shape, found nothing, discarded the error, and reported both categories as already in sync no matter how far apart the two instances were. Present since multi-instance support shipped in v0.5.0 (26 June 2026).
- **`pihole_config_get_value` returned the object enclosing the value rather than the value.** The same nesting: asking for `dns.hosts` printed `{"hosts": [...]}` instead of the list itself. The reply is now unwrapped down to the element requested, and the element is echoed back in the dotted spelling the caller used rather than the slash-separated path form. Present since v0.2.0 (2 May 2026).
- **A missing required parameter on twelve domain, group, client and list write tools was silently sent to Pi-hole rather than rejected.** `type`, `kind`, `domain`, `name`, `client` and `address` were each read with `req.RequireString(key)` and the error discarded, so a caller who forgot one had it treated as an empty string and forwarded, which for the domain tools built a malformed request path. All twelve sites now report which parameter is missing before anything is sent. Present since v0.1.0 (6 April 2026).
- **`pihole_auth_revoke_session` and `pihole_network_delete_device` treated a supplied `id` of `0` as though the argument were missing.** Both read it with `req.GetFloat("id", 0)`, which cannot distinguish a caller-supplied 0 from an absent argument, and then required it to be strictly positive. Session and device IDs are FTL-assigned integers that can legitimately be 0, so neither tool could ever target it. Both now use `req.RequireInt` and reject only a negative value, matching the pattern already used by `pihole_info_dismiss_message`. Present since v0.1.0 (6 April 2026) for sessions, v0.3.0 (7 May 2026) for devices.
- **`max_devices`, `max_addresses` and `max_results` accepted a negative value and sent it straight to Pi-hole.** None of the three was validated, unlike every other capped count in the tool set. They are now routed through the same bounds-checking helper as `limit`, `offset` and `count`, and rejected with the parameter named rather than forwarded. Present since v0.1.0 (6 April 2026) for `max_results`, v0.3.0 (7 May 2026) for the network parameters.
- **`pihole_teleporter_import` opened whatever `file_path` it was given with no validation.** It is the only tool parameter passed straight to `os.Open`, and a relative path, a non-`.zip` file, a directory, or an arbitrarily large file all reached the open call and failed there, if they failed at all, with whatever error the filesystem happened to return. The path is now checked upfront: absolute, `.zip`, a regular file, and under a 512 MiB cap, before the multipart upload is even assembled. Present since v0.1.0 (6 April 2026).
- **Backup files from `pihole_teleporter_export` and `pihole_instance_sync`'s pre-sync snapshot accumulated in the system temp directory forever.** Neither tool ever cleaned up after itself, so a server left running for weeks could fill its temp filesystem with backups nobody asked to keep. Both now reap files older than 24 hours matching their own naming pattern before writing a new one.

## [v0.8.1] - 2026-08-30

### Highlights

`pihole_config_set` has never applied a configuration change. From v0.1.0 through v0.8.0 the tool sent its payload as the bare `PATCH /api/config` body, and Pi-hole rejects that with `No "config" object in body data`, so every write failed on the wire. If you have been reading Pi-hole's configuration through this server but changing settings in the web UI, this is why. It is fixed, and the fix was found, diagnosed and contributed by [@st0aty](https://github.com/st0aty).

The rest of the release closes the ground around it. A payload that already carries the `config` envelope, which was the only workaround available while the bug was live, is now recognised rather than wrapped a second time. The doubled form is the dangerous one, because FTL answers 200 to keys it does not recognise and the write silently applies nothing. Input that is not a JSON object is now rejected against the documented parameter shape instead of travelling to the API to fail there. And `config_set`, previously the only configuration tool missing from the end-to-end suite, is now covered by it, which is precisely why this went unnoticed for eight releases.

### Fixed

- **`pihole_config_set` failed on every configuration write.** The handler sent the supplied object as the bare `PATCH /api/config` body, but Pi-hole requires it wrapped in a `config` key and rejects anything else with `No "config" object in body data`, so the tool has never applied a change. The payload is now wrapped before it is sent. Malformed JSON is also reported as a tool error up front rather than surfacing as an opaque API failure. Present since v0.1.0. Found and fixed by [@st0aty](https://github.com/st0aty) ([#48](https://github.com/hexamatic/pihole-mcp/pull/48)).
- **`pihole_config_set` accepts a payload that already carries the `config` envelope.** Anyone who worked around the bug above did so by passing `{"config": {...}}`, which was the one shape that reached Pi-hole intact. Wrapping that a second time produced `{"config": {"config": {...}}}`, and because FTL ignores keys it does not recognise and still answers 200, the write reported success while changing nothing. A lone `config` key is now treated as the envelope rather than a section, so both forms behave identically. Pi-hole has no top-level `config` section, so there is no ambiguity to resolve.
- **`pihole_config_set` rejects JSON that is not an object.** An array or a bare scalar previously travelled to the API and came back as `No "config" object in body data`, which described the transport rather than the mistake.

## [v0.8.0] - 2026-07-25

### Highlights

This release is about being findable and being verifiable. pihole-mcp is now listed in the **official [MCP Registry](https://registry.modelcontextprotocol.io/)** as `io.github.hexamatic/pihole-mcp` — the index MCP clients and directory aggregators read — so it can be installed by name, with every configuration variable described and the required ones flagged, rather than found by chance on GitHub. Ownership of the listing is proved cryptographically against the published container image on each release.

It is also the first release whose artefacts you can actually check. Keyless [cosign](https://docs.sigstore.dev/) signatures, SPDX SBOMs and SLSA build provenance have been wired up since v0.7.0 but never yet exercised by a tag; from v0.8.0 every checksum file, archive and container image ships with them, and [SECURITY.md](https://github.com/hexamatic/pihole-mcp/blob/main/SECURITY.md#verifying-release-artefacts) gives you the one-line command to verify each. Behind that, CodeQL now scans both the Go source and the CI workflows, container base images are pinned by digest so a rebuild cannot drift, and patch coverage has become a merge gate rather than a suggestion.

Homebrew users should notice nothing except a smoother first run: the tap now ships a cask instead of a deprecated formula, installs on macOS and Linux exactly as before, and clears the macOS quarantine attribute so the binary no longer trips Gatekeeper.

### Added

- **Listed in the official [MCP Registry](https://registry.modelcontextprotocol.io/) as `io.github.hexamatic/pihole-mcp`.** Clients that support registry install can add pihole-mcp by name and will prompt for `PIHOLE_URL` and `PIHOLE_PASSWORD`, with the optional settings (`TZ`, `PIHOLE_TLS_SKIP_VERIFY`, timeouts and retries) described alongside their defaults. The listing is published automatically on each tag and points at the `ghcr.io` image.
- **Signed, attested releases.** Release artefacts are now signed with keyless [cosign](https://docs.sigstore.dev/) (checksum file and Docker images), ship SPDX SBOMs for every archive, and carry SLSA build provenance verifiable with `gh attestation verify`. [SECURITY.md](https://github.com/hexamatic/pihole-mcp/blob/main/SECURITY.md#verifying-release-artefacts) documents every verification command. The checksum-file signature ships as a single Sigstore bundle, `pihole-mcp_0.8.0_SHA256SUMS.sigstore.json`, which carries the signature, the signing certificate and the transparency-log inclusion proof together — verify it with `cosign verify-blob --bundle` (cosign v3 or newer).
- **OpenSSF Scorecard** — a weekly supply-chain security analysis now runs against the repository, publishes its score to [scorecard.dev](https://scorecard.dev/viewer/?uri=github.com/hexamatic/pihole-mcp), and feeds the new README badge. All GitHub Actions across every workflow are now pinned by commit SHA (maintained automatically by Dependabot).

### Changed

- **Homebrew now installs a cask rather than a formula.** `brew install hexamatic/tap/pihole-mcp` is unchanged and still works on both macOS and Linux; existing installations migrate automatically on `brew upgrade`. On macOS the cask now clears the quarantine attribute during install, so the binary no longer trips Gatekeeper on first run. GoReleaser deprecated binary formulae in v2.10 — the cask is the supported form and covers both platforms.
- **README badge lineup refreshed.** The Go Report Card badge has been removed — the service was sunset on 1 July 2026 — and replaced with OpenSSF Scorecard and Go Reference (pkg.go.dev) badges. Its lint-quality role has long been covered by golangci-lint in CI.

### Security

- **Two dependency advisories resolved** — `golang.org/x/text` to v0.39.0 ([CVE-2026-56852](https://pkg.go.dev/vuln/GO-2026-5970), infinite loop on invalid input) and `golang.org/x/net` to v0.56.0 ([CVE-2026-46600](https://pkg.go.dev/vuln/GO-2026-5942), panic parsing a malformed SVCB or HTTPS DNS record). Neither was reachable from pihole-mcp's own code paths, so no released version was exploitable through this server; both are fixed regardless.
- **CodeQL static analysis** now runs on every push, every pull request, and weekly, over both the Go source and the GitHub Actions workflows. golangci-lint covers style and a good deal of correctness but is not a taint-tracking engine; CodeQL finds the dataflow issues a linter structurally cannot.
- **Container base images are pinned by digest** as well as by tag, so a rebuild cannot silently pick up a different `golang:1.26-alpine` or `distroless/static-debian13`. Dependabot maintains the digests.
- **Patch coverage is now an enforced gate** rather than advisory, and `govulncheck`, `gitleaks`, the fuzz smoke and the changelog check are required to pass before merge.

### Dependencies

- `golang.org/x/text` 0.37.0 → 0.39.0, `golang.org/x/net` 0.55.0 → 0.56.0, `golang.org/x/sys` 0.45.0 → 0.46.0 (see Security above)
- `google.golang.org/grpc` 1.81.1 → 1.82.1
- GitHub Actions: `actions/setup-go` v6 → v7, `codecov/codecov-action` v5 → v7, `actions/attest-build-provenance` v3 → v4

### Installation

**MCP Registry** — add by name in any client that supports registry install:
```
io.github.hexamatic/pihole-mcp
```

**Homebrew (macOS and Linux):**
```
brew install hexamatic/tap/pihole-mcp
```

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.8.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.8.0
```

**Binary download:** grab the archive for your platform from the release assets. Every archive is checksummed, cosign-signed, and ships an SPDX SBOM and SLSA provenance — see [SECURITY.md](https://github.com/hexamatic/pihole-mcp/blob/main/SECURITY.md#verifying-release-artefacts) to verify before you run it.

| Variant | Binary | Download | Docker image |
| ------- | ------ | -------- | ------------ |
| Default | 16.8 MB | 6.4 MB | 18.7 MB |
| Slim (no OpenTelemetry) | 9.6 MB | 3.8 MB | 11.5 MB |

### Requirements

- Pi-hole v6.6+ with the REST API enabled (verified against FTL v6.7)
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)
- Docker, if installing via the MCP Registry listing — it points at the container image

No configuration changes in this release. See the [README](https://github.com/hexamatic/pihole-mcp#readme) for the full variable reference and client-specific setup guides.

## [v0.7.0] - 2026-07-19

### Highlights

This release makes every timestamp unambiguous. Tool output used to render times with no timezone marker, in whatever zone the server happened to be running in — which, in the published Docker image, silently meant UTC: the distroless base ships no timezone database, so even setting `TZ` on the container did nothing ([#23](https://github.com/hexamatic/pihole-mcp/discussions/23)). **Every timestamp now carries an explicit zone marker, and the IANA timezone database is embedded in the binary itself**, so `TZ=Australia/Adelaide` on the container — or on any of the native binaries, Windows included — renders query logs in your local time out of the box. Left unset, output is UTC and says so.

Alongside that: an explicit opt-in for Pi-holes serving self-signed HTTPS certificates (`PIHOLE_TLS_SKIP_VERIFY`), and a round of hardening that has been on the list since v0.4.0 — coverage reporting, fuzz testing of the input validators, full-history secret scanning with gitleaks, and a [generated tool reference](https://github.com/hexamatic/pihole-mcp/blob/main/docs/TOOLS.md) that CI keeps honest so the docs can never again drift from the code.

### Added

- **`TZ` timezone support for rendered timestamps** ([#23](https://github.com/hexamatic/pihole-mcp/discussions/23)). Set `TZ` to an IANA zone (e.g. `Australia/Adelaide`) and every timestamp in tool output renders in that zone. The IANA timezone database is embedded in the binary, so this works in the Docker image — and on Windows — with no extra packages or volume mounts. An unrecognised `TZ` logs a startup warning and falls back rather than refusing to start.
- **`PIHOLE_TLS_SKIP_VERIFY`** (default `false`) — opt-in for Pi-hole instances serving self-signed HTTPS certificates. Verification stays on by default; the README documents why a trusted certificate is the better fix.
- **Full generated tool reference** at [docs/TOOLS.md](https://github.com/hexamatic/pihole-mcp/blob/main/docs/TOOLS.md) — every tool with its parameters, produced from the registered tool definitions by `cmd/toolsdoc` and checked for drift in CI, so it cannot go stale.
- **Coverage reporting** via Codecov on every push and pull request.
- **Secret scanning** with gitleaks, both as a pre-commit hook and as a full-history CI scan.
- **Fuzz testing** for the tool-parameter validators, run continuously in CI alongside the existing table-driven tests.

### Changed

- **Every timestamp now carries an explicit zone marker** (e.g. `19 Jul 2026, 9:41 AM UTC` rather than `19 Jul 2026, 9:41 AM`). Previously the Docker image silently rendered timestamps in unlabelled UTC — ambiguous for both people and AI agents trying to convert times ([#23](https://github.com/hexamatic/pihole-mcp/discussions/23)).

## [v0.6.0] - 2026-07-12

### Highlights

This release is about the things that go wrong. Pi-hole's embedded web server drops connections under load, and until now pihole-mcp passed that straight through — a dropped connection became a failed tool call. **Requests are now retried with exponential backoff**, but deliberately not uniformly: measured against FTL v6.7, the 429 you are actually most likely to meet is `api_seats_exceeded`, and it is not a rate limit at all. Pi-hole allows only **16 concurrent API sessions by default**, every client that logs in takes one, and a seat is freed only when a session times out half an hour later. Retrying it is futile, so pihole-mcp doesn't — it tells you how to fix it, pointing you at the tools that list and revoke sessions. This is the most frequently reported Pi-hole v6 API problem and it used to surface as an opaque error.

Two bugs surfaced from running the server against a real Pi-hole rather than a mock. **`pihole_info_messages` had never shown the text of a diagnostic message** — FTL returns it under `plain`, we read a key that does not exist, and every warning rendered as a type and a timestamp with nothing between them. It hid for five releases because a healthy Pi-hole reports no messages at all, so no test ever covered a populated response. And **configuring a second Pi-hole silently stripped the output schema from every tool that had one** — the flagship multi-instance feature quietly degrading the structured-output support it should have showcased.

On security: `golang.org/x/net` was carrying **seven advisories**, and the Go toolchain a reachable one in `crypto/tls`. All are fixed here. None of it was visible, because there was no vulnerability scanning in CI and every automated dependency update had been failing its checks since the day those checks were added. Both problems are now fixed, and `govulncheck` runs on every push, every pull request, and weekly — because advisories get disclosed against code that hasn't changed.

Verified end to end against **Pi-hole FTL v6.7**.

### Added

- **`pihole_info_dismiss_message`** — dismiss an FTL diagnostic message once you have dealt with it. Previously you could see Pi-hole's warnings but not clear them, so the `daily_report` and `security_audit` prompts had to send you to the web interface. `pihole_info_messages` now prints each message's ID to pass to it.
- **Automatic retry with exponential backoff and jitter** for transient Pi-hole failures. `PIHOLE_MAX_RETRIES` (default `3`, `0` disables) and `PIHOLE_RETRY_MAX_DELAY` (default `8s`).
- **A distinct, actionable error when Pi-hole's API session pool is full**, explaining that the limit is `webserver.api.max_sessions` (default 16), that every client takes a seat, and that you can free one with `pihole_auth_sessions` and `pihole_auth_revoke_session`.
- **Per-instance resources.** With more than one Pi-hole configured, each is addressable at `pihole://<instance>/status` and `pihole://<instance>/summary`, with a `pihole://instances` index. The unprefixed URIs still read the first-declared instance.
- README sections for **Troubleshooting** (session exhaustion, auth failures, Docker networking, dropped connections) and **Resources**, which had never been documented despite being advertised.

### Changed

- Verified against **Pi-hole FTL v6.7**; the development and CI containers now run `2026.07.2`.
- The SSE transport is **documented as deprecated**, in line with the MCP specification superseding HTTP+SSE with Streamable HTTP. It still works and still receives fixes; new deployments should use `-transport http`.
- Retries are **method-aware**. A rate-limited request is safe to replay for any method, because Pi-hole rejected it before processing it. A connection that failed without a reply is only replayed for reads — when we cannot know whether Pi-hole applied a delete, a duplicated delete is worse than the error it would have papered over.
- `just refresh-fixtures` now **seeds the development Pi-hole first**. A newly created Pi-hole has no query history, so every statistics endpoint answers empty and fixtures captured from it assert nothing.
- Documented artefact sizes are now **measured rather than remembered**. The long-repeated "9 MB Docker image" was wrong — that was roughly the binary size. Default: 16.4 MB binary, 6.1 MB download, 18.2 MB image. Slim: 9.2 MB, 3.6 MB, 11.8 MB.

### Fixed

- **`pihole_info_messages` displayed no message text.** FTL returns the body under `plain`; the client decoded a `message` key the API does not send, so every diagnostic warning rendered as a bare type and timestamp. Present since the tool was introduced.
- **Configuring a second Pi-hole stripped structured output from every tool.** A multi-instance tool can return either a single-instance result or an `instance=all` aggregate, and rather than describe both, the output schema was discarded entirely — so nine tools advertised a schema on one Pi-hole and none did on two. Both shapes are now declared as a `oneOf`.
- **A 5xx during login reported itself as "authentication failed".** A server that cannot answer is not a bad password.
- `DoRaw` and `PostMultipart` bypassed the 401 re-authentication path that every other request had.
- **Automated dependency updates had been failing CI since the checks were introduced.** The changelog enforcer skipped on labels that did not exist in the repository, so no bot pull request ever carried one; and commitlint's 100-character body limit tripped on the single unwrappable line Dependabot writes to name every module in a grouped update. Ungrouped updates passed, which made the failure look intermittent rather than systematic.
- Intermittent `sending auth request: EOF` failures in CI. The end-to-end suite starts a process per tool call, each authenticating afresh, which exhausts FTL's session table faster than a flat 0.3-second retry could outlast.
- `README` claimed nine prompts while listing six. `security_audit`, `weekly_trends` and `upstream_health` shipped in v0.2.0 and had been invisible to users ever since.

### Security

- **`golang.org/x/net` 0.52.0 → 0.55.0**, fixing GO-2026-4918, GO-2026-5025, GO-2026-5026, GO-2026-5027, GO-2026-5028, GO-2026-5029 and GO-2026-5030. **`golang.org/x/sys`** likewise fixes GO-2026-5024.
- **Go toolchain pinned to 1.26.5**, fixing GO-2026-5856 — an Encrypted Client Hello privacy leak in `crypto/tls` that `govulncheck` reports as reachable from the HTTP transport and the Pi-hole client's TLS paths.
- **`govulncheck` added to CI**, on every push and pull request plus a weekly schedule. The schedule is the point: advisories are disclosed against code that has not changed, so scanning only on push leaves a quiet repository silently vulnerable.

### Dependencies

- `github.com/mark3labs/mcp-go` 0.54.1 → 0.56.0
- `go.opentelemetry.io/otel` and friends 1.43.0 → 1.44.0
- `github.com/grpc-ecosystem/grpc-gateway/v2` 2.28.0 → 2.29.0
- `actions/checkout` 6 → 7
- Go toolchain 1.26.4 → 1.26.5

## [v0.5.0] - 2026-06-26

### Highlights

This release makes pihole-mcp a first-class tool for households and homelabs that run more than one Pi-hole. **Multiple instances** can now be configured side by side (`PIHOLE_1_URL`, `PIHOLE_2_URL`, …); every tool takes an optional `instance` argument, results are labelled with their source instance, and read-only tools accept `instance=all` to query the whole fleet concurrently and return a single structured aggregate. Two new tools turn the server into a safe alternative to standalone sync utilities: **`pihole_instance_diff`** reports exactly how two Pi-holes differ (adlists, allow/deny rules, groups, clients, local DNS), and **`pihole_instance_sync`** reconciles a target towards a source as a dry-run plan you confirm before anything is written — one direction only, host-specific and secret settings never touched. A new **`pihole_padd` dashboard tool** collapses what used to be half a dozen calls — queries, blocking state, top domain/client, cache, versions, and host health — into one snapshot, making it the natural first call for any status check. Alignment with the latest MCP best practices deepens across the board: every tool now carries a human-readable **title** with consistent behaviour hints, long-running actions emit **progress notifications**, the server emits **structured log messages** (with credential redaction), and the `investigate_domain` prompt offers **argument completions** sourced from your live domain rules. Tracks the current Pi-hole release (FTL v6.6.2 / docker `2026.05.0`) and toolchain (mcp-go v0.54.1).

### Added

- **Multi-instance support** — configure several Pi-holes with `PIHOLE_1_URL`/`PIHOLE_1_PASSWORD` (optional `PIHOLE_1_NAME`), `PIHOLE_2_URL`, and so on. The single-instance `PIHOLE_URL`/`PIHOLE_PASSWORD` form is unchanged and is named `primary`. Every tool gains an optional `instance` argument (advertised on the schema only when more than one instance is configured) and labels its result with the source instance. Read-only tools also accept `instance=all`, which now queries every instance **concurrently** and returns a **structured aggregate** (`summary` counts plus a per-instance array, each entry labelled and carrying its own data or error) alongside the `### instance: <name>` text fallback; a failure on one instance no longer fails the whole call. State-changing tools reject `instance=all`. The first-declared instance is the default and backs all resources.
- **`pihole_instance_diff`** — compare configuration between two instances (groups, adlists/allowlists, allow/deny exact and regex rules, clients, local DNS A/AAAA records, and CNAME records) and report what is added, changed, or only on the target. Read-only; runs no writes. Only registered when more than one instance is configured.
- **`pihole_instance_sync`** — reconcile a target Pi-hole towards a source, one direction only. Runs as a dry-run **plan** by default and returns a `confirm_token`; re-run with `mode=apply` and that token to apply. The token is derived from the planned changes, so a configuration that drifts between planning and applying is rejected rather than silently overwritten. Adds and updates by default; deletions require `prune=true`. A teleporter backup of the target is taken before any change (disable with `snapshot=false`). Host-specific and identity/secret settings (DHCP, interface bindings, passwords, TLS, sessions, 2FA) are never synced; group-membership associations are not synced because Pi-hole group IDs are instance-local.
- **`pihole_padd`** — a single-call dashboard snapshot (queries incl. `query_frequency`, blocking state, top domain/blocked/client, recent blocked, cache counters, FTL CPU/memory, CPU temperature, component versions, and — at `detail=full` — the primary network interface and host model). Structured output schema included. Recommended as the first call for a status overview.
- **Tool titles** — all 77 tools now set a human-readable `title` annotation (e.g. "Dashboard Snapshot", "Top Domains") distinct from the programmatic name, which MCP clients can surface in their UI.
- **Progress notifications** — `pihole_action_gravity_update` streams `notifications/progress` as the gravity rebuild proceeds (when the client supplies a `progressToken`); the flush actions emit start/complete progress.
- **MCP logging** — the server now emits `notifications/message` log events for notable operations (gravity update lifecycle, DNS restart, log/network flush, configuration changes), tagged with the originating instance. Credential-bearing fields are redacted before delivery, and the SDK gates delivery by the client's configured log level.
- **Prompt argument completions** — `completion/complete` is now supported; the `investigate_domain` prompt completes its `domain` argument from the configured allow/deny rules on the default instance.
- **Structured output schemas** added to `pihole_info_system`, `pihole_stats_top_domains`, and `pihole_stats_top_clients` (structured content is emitted even when `format=csv`).
- Development tooling for multi-instance work: a second Pi-hole behind the Compose `multi` profile, plus `just dev-up-multi` / `just dev-down-multi` and a multi-instance section in the E2E suite (gated on `PIHOLE_2_URL`).
- A Docker-free, in-process Pi-hole emulator (`internal/pihole/piholefake`) that backs the unit tests for routing, aggregation, and sync, plus a `just sim` walkthrough that runs the full plan→apply→converge flow locally without containers. CI now starts a second Pi-hole and runs the multi-instance integration tests and E2E suite against both.

### Fixed

- E2E harness: a parameter-default expansion (`${2:-{}}`) appended a stray `}` to every supplied tool-argument payload, so multi-instance E2E calls sent malformed JSON. The harness now also retries transient transport failures (a dropped connection under rapid-fire load is not a tool failure) and isolates the single- and multi-instance environment forms, making the suite a reliable CI gate.

### Changed

- Server instructions now recommend `pihole_padd` as the entry point and document the Pi-hole FTL v6.6 / v6.5 configuration keys (`resolver.macNames`, `database.forceDisk`, `dns.cache.rrtype`) that are settable via `pihole_config_set`.
- Tool count is now **77** (was 74): added `pihole_padd`, `pihole_instance_diff`, and `pihole_instance_sync` (the latter two appear only in multi-instance setups).
- Behaviour annotations are now internally consistent: read-only tools are no longer also flagged destructive or open-world (mcp-go's `NewTool` defaults both to true), so MCP clients render accurate hints. A test locks this invariant in across every tool.

### Dependencies

- Bumped `github.com/mark3labs/mcp-go` v0.54.0 → v0.54.1.
- Development and CI Pi-hole image pinned to `pihole/pihole:2026.05.0` (FTL v6.6.2 / Core v6.4.2 / Web v6.5).
- CI golangci-lint pinned v2.11 → v2.12; release workflow Docker actions bumped to v4 (Node 24 runtime).

## [v0.4.0] - 2026-05-23

### Highlights

This release hardens the HTTP and SSE transports so that pihole-mcp can safely be exposed beyond stdio — a per-session token-bucket rate limiter and an Origin/Host validator now wrap every HTTP and SSE request, matching the DNS-rebinding protection that the MCP 2025-11-25 specification recommends and that the reference Go SDK has shipped since early 2026. Defaults protect loopback only, so existing setups stay safe; LAN exposure now needs an explicit `PIHOLE_ALLOWED_ORIGINS` extension. Alongside the security work, every mutating tool now validates user-supplied domains, URLs, and free-form strings before any Pi-hole API call. A new `pihole_config_properties` tool surfaces the read-only config keys introduced in Pi-hole FTL v6.6.1 (tool count 73 → 74), and a new `slim` build tag strips OpenTelemetry support to drop the binary ~45% (17 MB → 9 MB stripped) for users who don't run a tracing backend. Both default and slim builds are now shipped as first-class release artefacts — same six platforms, separate tarball and Docker tag families.

### Added

- **HTTP and SSE transport hardening** — two new middlewares wrap the `http` and `sse` handlers (stdio is unaffected):
  - **Rate limiting** — per-session token bucket keyed by `Mcp-Session-Id` (fallback to client IP), default 120 req/min with burst `max(perMinute/4, 30)`. Configure via `PIHOLE_RATE_LIMIT`; `0` disables. Throttled requests return HTTP 429 with `Retry-After: 1`.
  - **Origin and Host validation** — DNS-rebinding protection per the MCP 2025-11-25 spec recommendation. Configure via `PIHOLE_ALLOWED_ORIGINS` (comma-separated). Default `localhost,127.0.0.1,[::1]`; the literal `*` disables (documented as unsafe). Missing `Origin` is allowed for non-browser clients (LibreChat, custom Go clients). Mismatches return HTTP 403.
- **`pihole_config_properties`** — new tool that lists configuration keys locked as read-only by `pihole.toml` or environment variable, with reason and human-readable description. Useful after a `pihole_config_set` rejection to confirm whether a key is intentionally immutable. Requires Pi-hole FTL v6.6.1+; the handler surfaces a friendly fallback error against older releases. Tool count is now **74**.
- **Slim build variant** — `go build -tags slim` (or `just build-slim`) excludes OpenTelemetry support entirely. The slim Linux amd64 binary drops from ~17 MB to ~9 MB stripped (~45% smaller; ~3.5 MB compressed vs ~6 MB). Both default and slim artefacts are now published for every release: tarballs as `pihole-mcp-slim_X.Y.Z_*` and Docker images as `:X.Y.Z-slim` / `:latest-slim`.
- **Input validation** at handler entry for every mutating tool — `pihole_domains_*`, `pihole_lists_*`, `pihole_clients_*`, `pihole_groups_*`, and `pihole_config_*`. Domain names are checked for RFC 1035 compliance (length, labels, no shell metacharacters); list URLs must parse as `http`/`https`/`file` with a non-empty host or path; comments and free-form names are length-capped (1024 / 255 characters). Invalid inputs now return a friendly MCP error before any Pi-hole API call is made, instead of surfacing a raw 400 from the Pi-hole server.
- **`format=csv`** added to `pihole_stats_recent_blocked`, `pihole_stats_query_types`, `pihole_stats_upstreams`, `pihole_stats_database_upstreams`, and `pihole_dhcp_leases`. Total CSV-capable tool count is now 15, saving ~30-40% tokens on large tables.

### Changed

- The `http` and `sse` transports now run inside a `net/http.Server` constructed by `cmd/pihole-mcp/main.go` rather than mcp-go's built-in `.Start()` helper. This is what allows the middleware chain to wrap the MCP handler. Behaviourally identical for clients that respect the existing graceful-shutdown signal handling. A `ReadHeaderTimeout` of 10 seconds is now enforced (mitigates slowloris).
- `pihole_history_graph` / `_history_clients` / `_history_database` / `_history_database_clients` descriptions now lead with the data source ("in-memory" vs "database") and cross-reference each other — removes the cognitive overhead of working out which tool you want from name alone.
- `pihole_network_info` description clarified to point users to `pihole_network_routes` / `pihole_network_interfaces` for richer per-route or per-interface detail.
- `pihole_config_set` is now annotated `openWorldHint: true` — the tool can affect DNS resolution and other services system-wide, and the hint surfaces that to MCP clients that gate destructive operations.

### Fixed

- Hardened `pihole_network_devices` against invalid UTF-8 bytes in the upstream `macVendor` field (Pi-hole FTL upstream issue [#2868](https://github.com/pi-hole/FTL/issues/2868)). Go's `encoding/json` already silently replaces non-UTF-8 sequences with U+FFFD during decode, so this MCP server was unaffected — a regression-prevention test is now in place to lock that behaviour in.

### Quality

- Fixture suite expanded from 13 → 22 captured Pi-hole API responses. `scripts/refresh-fixtures.sh` now also captures the full stats family (`top_domains`, `top_clients`, `upstreams`, `query_types`, `recent_blocked`), the four `stats_database_*` endpoints, and `config_properties` (skipped on older Pi-hole versions that return an empty body for the endpoint).
- New `_RealFixture` shape-validation tests across the stats and auth surfaces. Each runs the handler against the captured response and confirms the handler doesn't crash and emits non-empty output. Hand-written value-assertion mocks remain in place for tests that pin specific numbers.

### Dependencies

- `github.com/mark3labs/mcp-go` bumped 0.47.0 → 0.54.0. Brings panic recovery to the SSE message handler, stdio worker, task goroutines, and session hook goroutines; adds a transport-agnostic `Handle` entry point; adds OpenTelemetry server-side tracing hooks; adds `WithStrictInputSchemaDefault`. No breaking changes for our usage — every `server.NewMCPServer`, `server.NewStreamableHTTPServer`, and `server.NewSSEServer` call site compiles and passes tests unchanged.
- `golang.org/x/time` v0.15.0 added as a direct dependency to back the rate-limit token bucket.

### Migration Notes

- **HTTP and SSE transports now enforce Origin and Host validation by default.** Requests are accepted only when the `Host` (and `Origin`, if present) header resolves to a host in `PIHOLE_ALLOWED_ORIGINS`. The default allowlist covers loopback (`localhost,127.0.0.1,[::1]`) only — if you're exposing pihole-mcp on a LAN or behind a reverse proxy, extend the list:
  ```sh
  export PIHOLE_ALLOWED_ORIGINS="localhost,127.0.0.1,[::1],pihole-mcp.lan"
  ```
  Set `PIHOLE_ALLOWED_ORIGINS=*` to disable the check entirely (only when behind a proxy doing its own access control). stdio is unaffected.
- **Per-session rate limiting is on by default** at 120 req/min with burst 30. If your client legitimately exceeds that during batch refreshes, raise `PIHOLE_RATE_LIMIT` (e.g. `600`) or set `PIHOLE_RATE_LIMIT=0` to disable.
- **Slim build variant is opt-in by archive or tag name** — defaults are unchanged. To pull the lean binary, grab `pihole-mcp-slim_0.4.0_*.tar.gz` from the release assets or the `ghcr.io/hexamatic/pihole-mcp:0.4.0-slim` Docker tag. `OTEL_EXPORTER_OTLP_ENDPOINT` is silently ignored in slim builds.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.4.0
```
For the slim variant (no OpenTelemetry): `go install -tags slim github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.4.0`

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.4.0           # default (includes OpenTelemetry)
docker pull ghcr.io/hexamatic/pihole-mcp:0.4.0-slim      # slim (~45% smaller, no OTel)
```

**Binary download:** grab the archive for your platform from the release assets — `pihole-mcp_0.4.0_{os}_{arch}.tar.gz` for the default build, `pihole-mcp-slim_0.4.0_{os}_{arch}.tar.gz` for the slim build.

### Requirements

- Pi-hole v6.6+ with the REST API enabled (v6.6.1+ for `pihole_config_properties`)
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                      | Required | Default                       | Description                                                                |
| ----------------------------- | -------- | ----------------------------- | -------------------------------------------------------------------------- |
| `PIHOLE_URL`                  | Yes      | —                             | Pi-hole base URL                                                           |
| `PIHOLE_PASSWORD`             | Yes      | —                             | Admin or application password                                              |
| `PIHOLE_REQUEST_TIMEOUT`      | No       | `30s`                         | HTTP request timeout                                                       |
| `PIHOLE_RATE_LIMIT`           | No       | `120`                         | Per-session requests/min cap on HTTP/SSE transports; `0` disables          |
| `PIHOLE_ALLOWED_ORIGINS`      | No       | `localhost,127.0.0.1,[::1]`   | Origin/Host allowlist for HTTP/SSE transports; `*` disables (unsafe)       |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No       | —                             | OpenTelemetry endpoint (enables tracing; ignored in slim builds)           |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline) and the [Security section](https://github.com/hexamatic/pihole-mcp#security-http-and-sse-transports) for the transport hardening details.

## [v0.3.0] - 2026-05-07

### Highlights

Five new tools complete the network-management and long-term-history surfaces, raising tool coverage to 73. A fixture-based test infrastructure was introduced and immediately surfaced three Pi-hole API shape bugs that had been silently masking incorrect data — all now corrected. Conventional Commits and `CHANGELOG.md` updates are now enforced both locally and in CI, and releases publish straight to GitHub Releases without a manual draft step.

### Added

- **`pihole_history_database`** — query the long-term FTL database for total query counts grouped by interval. `from`/`until` parameters scope the window and default to the last 7 days when omitted.
- **`pihole_history_database_clients`** — per-client breakdown of long-term query history over the same windowed interval.
- **`pihole_network_routes`** — list every routing-table entry observed by FTL with family, scope, and source attribution.
- **`pihole_network_interfaces`** — list network interfaces with link state, speed, addresses, and per-interface byte counters.
- **`pihole_network_delete_device`** — remove a stale device record from the FTL network table.
- Fixture-based testing harness under `testdata/fixtures/` (13 captured Pi-hole responses) plus a `loadFixture(t, name)` helper in `internal/tools/fixtures_test.go`. Fixture refresh is automated via `scripts/refresh-fixtures.sh` and the `just refresh-fixtures` recipe.
- `RELEASING.md` runbook documenting the tag-driven release procedure.

### Changed

- `pihole_history_graph` and `pihole_history_clients` now operate exclusively on in-memory FTL data. The overloaded `from`/`until` parameters that previously routed those tools through the long-term database have been removed — that path now lives in the dedicated `pihole_history_database*` tools above.
- `internal/pihole/types.go` — `DatabaseInfo`, `NetworkInterface`, and `NetworkInterfaceStats` were updated to match the actual Pi-hole v6 wire format observed in fixtures (see Fixed below).

### Fixed

- **`/api/info/database`** — the response is flat at the top level, not wrapped in a `database` key. The previous shape silently returned all-zeros for SQLite version, file size, and timestamp fields. `DatabaseInfo` is now flat and the affected handler renders correct values.
- **`info_database.earliest_timestamp`** — Pi-hole emits `float64` (sub-second precision), not `int64`. Type updated; previous decode path discarded fractional seconds and could fail on values that exceeded the int range.
- **`/api/network/interfaces[].speed`** — nullable for loopback and tunnel interfaces. Now `*int`; previous non-pointer field caused JSON decode errors against real Pi-hole instances.
- **`/api/network/interfaces[].addresses[].prefixlen`** — corrected from `prefix` to `prefixlen` to match the Pi-hole field name.
- **`/api/network/interfaces[].stats.rx_bytes` / `tx_bytes`** — wire format is `{unit, value}` envelopes, not raw integers. Type updated and `format.Bytes()` rendering applies.

### Release Pipeline

- `.goreleaser.yaml` now sets `release.draft: false` and `release.mode: keep-existing`. Tags publish straight to GitHub Releases with no manual draft step, and re-runs do not overwrite already-published bodies.
- Conventional Commits are enforced locally via a lefthook `commit-msg` hook (zero deps, pure shell regex) and on PRs via `wagoid/commitlint-github-action`.
- `CHANGELOG.md` updates are enforced on PRs via `dangoslen/changelog-enforcer`. The `Skip-Changelog` label exists for legitimately internal-only PRs (refactors, tests, CI, dev tooling, no-op dependency bumps).
- `scripts/release-notes.sh` extracts each release body from this file and feeds it to goreleaser via `--release-notes=NOTES.md`, replacing the auto-generated changelog block.
- `scripts/changelog-draft.sh` scaffolds the next `[Unreleased]` section from `git log` when prepping a release.

### Migration Notes

- **`pihole_history_graph` and `pihole_history_clients`** no longer accept `from` / `until` parameters. Long-term database queries that previously routed through these tools are now served by the dedicated `pihole_history_database` and `pihole_history_database_clients` tools, both of which default to a 7-day window when `from` / `until` are omitted.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.3.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.3.0
```

**Binary download:** grab the archive for your platform from the release assets.

### Requirements

- Pi-hole v6.6+ with the REST API enabled
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                  | Required | Default | Description                       |
| ------------------------- | -------- | ------- | --------------------------------- |
| `PIHOLE_URL`              | Yes      | —       | Pi-hole base URL                  |
| `PIHOLE_PASSWORD`         | Yes      | —       | Admin or application password     |
| `PIHOLE_REQUEST_TIMEOUT`  | No       | `30s`   | HTTP request timeout              |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline) and the [OpenTelemetry section](https://github.com/hexamatic/pihole-mcp#opentelemetry) for opt-in tracing configuration.

## [v0.2.0] - 2026-05-02

### Highlights

The repository moved from `lloydmcl/pihole-mcp` to `hexamatic/pihole-mcp` — existing GitHub URLs redirect, stars, watchers, issues, and forks remain in place; new container images publish to `ghcr.io/hexamatic/pihole-mcp`. This release also lands the v0.2.0 feature set: twelve new tools (68 total, ~95% Pi-hole v6 REST API coverage), three new MCP prompts (nine total), structured output schemas on key tools per the MCP 2025-11-25 specification, and sixteen new unit-test files raising coverage across all 17 tool categories.

### Added

- **Long-term database statistics** — four new tools surface historical analysis over the FTL database with mandatory `from`/`until` windows:
  - `pihole_stats_database_top_domains`
  - `pihole_stats_database_top_clients`
  - `pihole_stats_database_upstreams`
  - `pihole_stats_database_query_types`
- **Granular configuration** — three new tools support dotted-path access into Pi-hole's hierarchical config and deferred FTL restart for batch operations:
  - `pihole_config_get_value`
  - `pihole_config_add_value`
  - `pihole_config_remove_value`
- **System info** — three new tools expose FTL engine internals, live DNS/DHCP metrics, and hardware temperature sensors (`pihole_info_ftl`, `pihole_info_metrics`, `pihole_info_sensors`).
- **Session management** — two new tools for security auditing: `pihole_auth_sessions` (list active sessions) and `pihole_auth_revoke_session` (terminate a session by ID).
- **MCP prompts** — three new prompts:
  - `security_audit` — reviews active sessions, authentication configuration, and diagnostic messages for unauthorised access patterns.
  - `weekly_trends` — week-over-week DNS statistics comparison via the new long-term database tools.
  - `upstream_health` — DNS resolver performance, cache efficiency, and DNSSEC validation analysis.
- **Structured output schemas** — `pihole_dns_get_blocking`, `pihole_stats_summary`, and `pihole_domains_list` now return structured output per MCP 2025-11-25, allowing downstream agents to parse results programmatically without LLM interpretation of formatted text.

### Changed

- **Repository transferred** from `lloydmcl/pihole-mcp` to `hexamatic/pihole-mcp`. Existing GitHub URLs redirect; the Go module path, container image references, badge URLs, and goreleaser configuration are rewritten to the new namespace across `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, the issue templates, and every Go source file.
- **Container images** now publish to `ghcr.io/hexamatic/pihole-mcp` (multi-arch: linux/amd64, linux/arm64).
- **Project branding** — replaced the Pi-hole Vortex logo with custom branding to avoid trademark conflicts.
- **Shared helpers** extracted across tool handlers (`toolError`, `getCountCapped`) centralise error formatting and count clamping that were previously duplicated.

### Fixed

- **`justfile` PATH composition** — `mise bin-paths` emits one path per line; the previous concatenation truncated `PATH` at the first entry, leaving `goreleaser` and other mise-managed tools unreachable from `just release-dry`. Now collapses newlines into the colon separator before prepending.

### Quality

- Sixteen new unit-test files raise tool-handler coverage across all 17 tool categories, including error paths and parameter validation.
- The E2E suite is extended to cover the twelve new tools and a config add/remove round-trip against a live Pi-hole.

### Migration Notes

- **Container image path** — pull from `ghcr.io/hexamatic/pihole-mcp:0.2.0` (or `:latest`). The previous `ghcr.io/lloydmcl/pihole-mcp` path is no longer published; existing images at the old path remain accessible but receive no updates.
- **Go module path** — `go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.2.0`. Existing imports of `github.com/lloydmcl/pihole-mcp` should be updated; the GitHub redirect handles the source pull, but Go's module proxy may cache under the new path.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.2.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.2.0
```

**Binary download:** grab the archive for your platform from the release assets.

### Requirements

- Pi-hole v6.6+ with the REST API enabled
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                  | Required | Default | Description                       |
| ------------------------- | -------- | ------- | --------------------------------- |
| `PIHOLE_URL`              | Yes      | —       | Pi-hole base URL                  |
| `PIHOLE_PASSWORD`         | Yes      | —       | Admin or application password     |
| `PIHOLE_REQUEST_TIMEOUT`  | No       | `30s`   | HTTP request timeout              |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline) and the [OpenTelemetry section](https://github.com/hexamatic/pihole-mcp#opentelemetry) for opt-in tracing configuration.

## [v0.1.0] - 2026-04-06

### Highlights

A production-grade MCP server for Pi-hole v6, providing complete API coverage through 55 tools, 6 prompts, and 5 resources — all in a single Go binary.

### Added

- **55 tools** across 16 categories: DNS blocking, statistics, queries, domains, groups, clients, lists, config, actions, network, DHCP, logs, and more.
- **6 MCP prompts** for guided workflows: DNS diagnosis, domain investigation, blocked domain review, network audit, blocklist optimisation, and daily reporting.
- **5 MCP resources** for quick status checks (`pihole://status`, `pihole://summary`, plus three URI templates for client/domain/list detail).
- **Response controls** — `detail` (`minimal`/`normal`/`full`) and `format` (`text`/`csv`) parameters on applicable tools, letting callers trade verbosity for token economy.
- **Session lifecycle management** — lazy login on first call, automatic re-authentication on 401 with compare-and-swap to avoid thundering herd, and session cleanup on shutdown to prevent FTL session-slot exhaustion.
- **Optional OpenTelemetry tracing** — opt-in via `OTEL_EXPORTER_OTLP_ENDPOINT` for end-to-end observability.
- **Transports** — stdio (default), HTTP (Streamable HTTP), and SSE.

### Installation

**Go install:**
```
go install github.com/hexamatic/pihole-mcp/cmd/pihole-mcp@v0.1.0
```

**Docker (multi-arch):**
```
docker pull ghcr.io/hexamatic/pihole-mcp:0.1.0
```

> **Note:** v0.1.0 was released under `lloydmcl/pihole-mcp` and `ghcr.io/lloydmcl/pihole-mcp`. Both URLs still resolve via GitHub redirects; the `hexamatic` paths above point at the same artefacts going forward. See v0.2.0 migration notes.

**Binary download:** grab the archive for your platform from the release assets.

### Requirements

- Pi-hole v6.6+ with the REST API enabled
- An admin password or [application password](https://docs.pi-hole.net/api/auth/)

### Configuration

| Variable                  | Required | Default | Description                       |
| ------------------------- | -------- | ------- | --------------------------------- |
| `PIHOLE_URL`              | Yes      | —       | Pi-hole base URL                  |
| `PIHOLE_PASSWORD`         | Yes      | —       | Admin or application password     |
| `PIHOLE_REQUEST_TIMEOUT`  | No       | `30s`   | HTTP request timeout              |

See the [README](https://github.com/hexamatic/pihole-mcp#readme) for client-specific setup guides (Claude Desktop, Cursor, Windsurf, VS Code, Cline).

[Unreleased]: https://github.com/hexamatic/pihole-mcp/compare/v0.8.1...HEAD
[v0.8.1]: https://github.com/hexamatic/pihole-mcp/compare/v0.8.0...v0.8.1
[v0.8.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.7.0...v0.8.0
[v0.7.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/hexamatic/pihole-mcp/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/hexamatic/pihole-mcp/releases/tag/v0.1.0
