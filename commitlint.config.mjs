// commitlint configuration for pihole-mcp.
//
// Enforces Conventional Commits (https://www.conventionalcommits.org/).
// Read by `wagoid/commitlint-github-action` in CI — see
// `.github/workflows/commitlint.yml`. Local enforcement is handled by
// commitsar (Go binary, mise-managed) wired into `lefthook.yml`'s
// pre-push hook, which keeps Node out of the local toolchain.
//
// The standard Angular type set is allowed: feat, fix, docs, test, ci,
// chore, refactor, perf, build, style, revert. Scopes are unrestricted, so
// Dependabot's `chore(deps):` subject line is accepted as-is.

export default {
    extends: ['@commitlint/config-conventional'],

    // Dependabot's *grouped* updates open with a single unwrappable body line
    // naming every bumped module and its URL — 114 characters for one update,
    // 268 for four. That trips config-conventional's `body-max-line-length` (100)
    // and permanently red-flags every grouped dependency PR, while ungrouped ones
    // pass. Exempt Dependabot's commits rather than relax the limit for humans,
    // who can and should wrap their prose.
    ignores: [(message) => /^Signed-off-by: dependabot\[bot\]/m.test(message)],
};
