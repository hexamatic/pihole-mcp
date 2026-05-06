// commitlint configuration for pihole-mcp.
//
// Enforces Conventional Commits (https://www.conventionalcommits.org/).
// Read by `wagoid/commitlint-github-action` in CI — see
// `.github/workflows/commitlint.yml`. Local enforcement is handled by
// commitsar (Go binary, mise-managed) wired into `lefthook.yml`'s
// pre-push hook, which keeps Node out of the local toolchain.
//
// The standard Angular type set is allowed: feat, fix, docs, test, ci,
// chore, refactor, perf, build, style, revert. Scopes are unrestricted —
// Dependabot's `build(deps):` / `chore(deps):` / `ci(deps):` all pass
// without further config.

export default {
    extends: ['@commitlint/config-conventional'],
};
