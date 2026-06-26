package prompts

import (
	"context"
	"sort"
	"strings"

	"github.com/hexamatic/pihole-mcp/internal/pihole"
	"github.com/mark3labs/mcp-go/mcp"
)

// maxCompletionValues caps returned completions per the MCP spec (<= 100).
const maxCompletionValues = 100

// CompletionProvider supplies argument completions for prompts. Currently it
// completes the investigate_domain "domain" argument from the configured
// allow/deny rules on the default Pi-hole instance — the values a user is most
// likely to investigate.
type CompletionProvider struct {
	registry *pihole.Registry
}

// NewCompletionProvider builds a prompt-argument completion provider backed by
// the instance registry.
func NewCompletionProvider(r *pihole.Registry) *CompletionProvider {
	return &CompletionProvider{registry: r}
}

// CompletePromptArgument implements server.PromptCompletionProvider.
func (p *CompletionProvider) CompletePromptArgument(ctx context.Context, promptName string, argument mcp.CompleteArgument, _ mcp.CompleteContext) (*mcp.Completion, error) {
	if promptName != "investigate_domain" || argument.Name != "domain" {
		return &mcp.Completion{Values: []string{}}, nil
	}

	var result pihole.DomainsResponse
	if err := p.registry.Default().Get(ctx, "/domains", &result); err != nil {
		// Completions are best-effort; never fail the request.
		return &mcp.Completion{Values: []string{}}, nil
	}

	prefix := strings.ToLower(argument.Value)
	seen := make(map[string]bool)
	var values []string
	for _, d := range result.Domains {
		if d.Domain == "" || seen[d.Domain] {
			continue
		}
		if prefix != "" && !strings.Contains(strings.ToLower(d.Domain), prefix) {
			continue
		}
		seen[d.Domain] = true
		values = append(values, d.Domain)
	}
	sort.Strings(values)

	total := len(values)
	hasMore := false
	if total > maxCompletionValues {
		values = values[:maxCompletionValues]
		hasMore = true
	}

	return &mcp.Completion{Values: values, Total: total, HasMore: hasMore}, nil
}
