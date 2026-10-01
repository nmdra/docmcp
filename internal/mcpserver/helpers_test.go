package mcpserver_test

import (
	"context"

	"github.com/docmcp/docmcp/internal/mcpserver"
	"github.com/docmcp/docmcp/internal/search"
)

// resolverFunc and searcherFunc let a test drive the two tools without a store
// or an index: the MCP layer's job is the contract, not retrieval.
type resolverFunc func(ctx context.Context, libraryName, query string) ([]search.Match, error)

func (f resolverFunc) Resolve(ctx context.Context, name, query string) ([]search.Match, error) {
	return f(ctx, name, query)
}

type searcherFunc func(ctx context.Context, req mcpserver.SearchRequest) (string, error)

func (f searcherFunc) Search(ctx context.Context, req mcpserver.SearchRequest) (string, error) {
	return f(ctx, req)
}
