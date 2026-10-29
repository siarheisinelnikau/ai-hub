# Contributing

Use English for documentation, UI copy, comments and CLI messages. Preserve
original languages in prompts, model responses and Unicode tests. Never commit
credentials, private network details, weights, state or production conversations.

## Development

Install Go 1.26.10+, Python 3.10+ and Node.js 22+:

```sh
python -m pip install -r requirements-dev.txt
python scripts/check.py
```

`python scripts/demo.py` previews the UI with synthetic data and disabled actions.
Run `go test -race -count=1 -timeout 120s ./...` on Linux for concurrency changes.
Tests must use temporary files and fake services, never production resources.

- Keep changes focused; add meaningful regression tests for behavior/security.
- Format Go with `gofmt`. Keep `static/css/main.css` and `static/js/main.css.js`
  identical and increment changed asset versions in `static/index.html`.
- Use explicit configuration instead of machine-specific addresses.
- Keep registry, catalog, wrappers, units, state, clients and benchmarks consistent.
  Deleting weights is not forgetting a profile.
- State what was tested and what still needs hardware validation.
- Keep the historical Python reference limited to compatibility tests.

Describe the concrete problem and resulting behavior in your pull request.
Document changed defaults. Performance claims need reproducible measurements.
