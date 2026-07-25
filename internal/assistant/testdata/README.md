# ptchan Context Golden Fixtures

These fixtures exercise Martie's assistant context rendering from sanitized
thread data returned by `ptchan-gateway`. The renderer is shared infrastructure for any Martie
surface that needs ptchan context, including Telegram link enrichment and the
planned ptchan-native assistant.

Each test case is a gateway thread read captured as JSON plus the prompt packet
Martie is expected to render from it. The goal is to make assistant context behavior easy
to audit by humans and other agents: the input is the gateway contract, and the
golden output is exactly what Martie sends as transient model context. Keep these
fixtures surface-neutral unless a test is deliberately covering a surface-specific
prompt wrapper.

## Files

For each case, add:

- `<case>.json`: a full sanitized `GET /integration/v1/threads/:board/:thread_id`
  response from `ptchan-gateway`.
- `<case>.golden`: the expected rendered Martie context packet.
- `<case>.meta`: optional rendering settings for the case.

Metadata uses this shape:

```json
{
  "target_post_id": 2948,
  "max_replies": 3
}
```

If metadata is omitted, Martie renders with no focus post and `max_replies = 25`.

## Local Real Captures

For real threads that are useful while developing but not ready to commit, put
fixtures under:

```text
internal/assistant/testdata/local/
```

That directory is ignored by git and excluded from the default golden test run.
Use it for raw development captures that still need a privacy and usefulness
review.

To include local fixtures in the golden test run:

```bash
MARTIE_INCLUDE_LOCAL_GOLDEN=1 go test ./internal/assistant -run TestPtchanContextGoldenFiles
```

To regenerate local and committed goldens together:

```bash
MARTIE_INCLUDE_LOCAL_GOLDEN=1 MARTIE_UPDATE_GOLDEN=1 go test ./internal/assistant -run TestPtchanContextGoldenFiles
```

Promote a local fixture by moving its `.json`, `.meta`, and `.golden` files out
of `local/` after reviewing that it is sanitized, useful, and safe to commit.

## Gateway Agent Instructions

When generating fixtures from `ptchan-gateway`, provide realistic sanitized
thread read responses only. Use the integration API contract, not upstream ptchan JSON.
The gateway is Martie's ptchan boundary; fixture data should reflect what an
integration is allowed to see, not what upstream might contain internally.

The JSON file should look like:

```json
{
  "board": "i",
  "thread_id": 100,
  "posts": [],
  "truncated": false
}
```

Each post may include:

```json
{
  "board": "i",
  "thread_id": 100,
  "post_id": 2948,
  "url": "https://ptchan.org/i/thread/100.html#2948",
  "date": "2026-07-23T10:48:00Z",
  "subject": "optional",
  "message": "optional",
  "name": "optional",
  "tripcode": "optional",
  "capcode": "optional",
  "donor": true,
  "country": "US",
  "poster_fingerprint": "optional",
  "attachment_count": 0,
  "references": [
    { "board": "i", "thread_id": 100, "post_id": 2943 }
  ],
  "referenced_by": [
    { "board": "i", "thread_id": 100, "post_id": 2950 }
  ]
}
```

Good fixtures should cover one focused behavior at a time:

- OP plus recent tail with no focus post.
- A focus post that references one or more earlier posts.
- Posts that reference the focus post.
- Missing referenced posts, so Martie marks them unavailable.
- `truncated: true`, so Martie warns about missing earlier context.
- Greentext lines beginning with `>`.
- Anonymous posts with no stable identity markers.
- Public identity markers such as `tripcode` or `capcode`.
- Attachment-only or empty-text posts.
- Very long post bodies that must be bounded per post.
- Cross-thread references, if the gateway emits them.
- Gateway-origin markers for Martie's own produced posts when self-reply
  avoidance is relevant to the scenario.

## Privacy Boundary

Fixtures must never include data outside the sanitized gateway integration
contract. Do not include raw IPs, upstream cloaks, moderation hashes, session
data, permission state, webhook secrets, raw upstream JSON, file names, file
hashes, or hidden poster identity.

Anonymous posts are not stable identities. Only include public labels or scoped
fingerprints if the gateway integration contract exposes them for that fixture.

## Updating Golden Output

After adding or intentionally changing a fixture, regenerate goldens with:

```bash
MARTIE_UPDATE_GOLDEN=1 go test ./internal/assistant -run TestPtchanContextGoldenFiles
```

Then review the `.golden` diff by hand. The golden file should read like a
clear prompt packet with stable sections:

- `PTCHAN FORMAT NOTES`
- `TASK`
- `CONVERSATION MAP`
- `THREAD TRANSCRIPT`
- `RESPONSE RULES`

Finally run:

```bash
make check
```
