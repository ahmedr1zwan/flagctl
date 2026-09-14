# v1 wire contract baseline

`contract.json` pins the existing request and response behavior at
[f57c5a4b869909f6d4cbbb607162ccd420d0655b](https://github.com/ahmedr1zwan/flagctl/commit/f57c5a4b869909f6d4cbbb607162ccd420d0655b),
before the additive response-ID change. Expectations are deliberately independent
of the current Go response structs and client implementation.

The cases run in order against one temporary SQLite-backed service. They verify
routes, methods, status codes, Location/Allow/content-type headers, defaults,
partial updates, environment isolation, list ordering, empty arrays, deletion,
and representative error envelopes. Existing API tests additionally cover all
error codes, payload limits, strict decoding, and storage failures.

Response objects may gain fields. Every baseline field must still exist with
the same type and value; array lengths and order remain exact. Two placeholders
avoid freezing runtime values outside the compatibility promise:

- `<utc-timestamp>` requires a non-zero UTC RFC3339 timestamp string. The archived
  client driver separately checks creation and no-op timestamp preservation.
- `<message>` requires a non-empty human-readable error string. Error status and
  code remain exact; message wording may change.

`null` as an entire fixture response means there must be no HTTP body, as with
HEAD and DELETE 204. It does not permit a literal JSON null response.

There is no automatic fixture-update command. Do not rewrite the baseline to
approve a breaking v1 change. Add separate assertions for new optional fields;
keep the old fixtures and historical client tests passing. Negative controls
verify that the matcher rejects missing fields, changed types/defaults/error
codes, null lists, reordered/truncated arrays, and invalid timestamps.
