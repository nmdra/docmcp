# Crawl policy

## Scope

A crawl stays on the source host and inside its base path. Includes narrow that
scope. Excludes take priority over includes. Robots rules apply before each fetch.

## Robots paths and URL normalization

Robots rules use literal path matching. DocMCP normalizes trailing slashes before
it fetches documentation pages.

For example, `Disallow: /docs/secret/` does not match the normalized request path
`/docs/secret`. DocMCP does not rewrite the robots rule to remove its slash.

`TestRobots_TrailingSlashInDisallowDoesNotCoverTheNormalizedPath` preserves this
behavior. A change to this interaction requires an explicit crawler policy
decision, not a change to an isolated test.

## Empty and partial crawls

`add` fails when a crawl produces no usable pages. It removes the new library
registration. This includes non-HTML pages and filters that exclude every page.

```bash
docmcp add https://example.com/docs/ --name example
```

A partial crawl can succeed when at least one page is usable. The ingestion
report shows skipped pages.

`sync` warns when a crawl produces no usable pages. It retains the library
registration. Retention of the registration does not guarantee retention of its
chunks: reconciliation removes chunks whose pages no longer appear.

```bash
docmcp sync example
```

The regression tests cover empty and non-HTML sources, partial crawls, and sync
that removes every indexed page while retaining the library.
