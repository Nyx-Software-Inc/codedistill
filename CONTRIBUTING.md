# Contributing to CodeDistill Community Edition

Thanks for your interest! Bug reports and pull requests are welcome.

## Contributor License Agreement (CLA)

CodeDistill is an open-core project: this Community Edition is
AGPL-3.0, and the same codebase also ships in a commercial edition.
So that both can continue to exist, we ask every contributor to agree
to a Contributor License Agreement before we can merge code:

> By submitting a contribution to this repository, you grant the
> CodeDistill maintainers a perpetual, worldwide, non-exclusive,
> royalty-free, irrevocable license to use, reproduce, modify,
> sublicense, and distribute your contribution as part of any
> CodeDistill edition, under any license, including commercial
> licenses — and you certify that you have the right to grant this
> license for the contributed work.

A CLA-assistant check on pull requests records your agreement the
first time you contribute.

## Ground rules

- Match the surrounding code's style and comment density.
- `go test ./...` and `npm run check` (under `web/`) must pass.
- This mirror is generated from a private monorepo per release;
  PRs are cherry-picked inward, so small focused changes merge fastest.
