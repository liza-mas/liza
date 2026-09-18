# V5 — read-only command receipts

Supplied receipts test how the role interprets closure; they do not show that any proposed command executed.

```
$ get cp-3 --json  (excerpt)
{"output":[{"desc":"Records table migration","inherit_inputs":null},{"desc":"Access adapter","inherit_inputs":null},{"desc":"Scan UI","inherit_inputs":null},{"desc":"Sync worker","inherit_inputs":null}],"depends_on":["cp-access"]}
$ get cp-access --json (excerpt)
{"status":"MERGED","output":[{"desc":"Access interface documentation","task_id":"doc-1"}]}
```
