<!-- reeve:pr-comment:v1 -->
## 🟡 reeve · preview · run #47 · [commit abc1234]

**4 stacks changed** · ⏱ 42s · [View run](https://example.com/runs/47)

| Stack | Env | ➕ Add | 🔄 Change | ➖ Delete | 🔁 Replace | Status |
|---|---|---|---|---|---|---|
| api/prod | prod | 2 | 1 | 0 | 0 | 🔒 blocked by #482 |
| api/staging | staging | 5 | 0 | 0 | 0 | ✅ ready |
| worker/prod | prod | 0 | 3 | 0 | 1 | ✅ ready |
| noop/dev | dev | 0 | 0 | 0 | 0 | · no-op |

⚠️ Replacements detected — review carefully.

---

### api/prod · prod · 🔒 blocked by #482

  Queued behind #482.

<details><summary>Summary (2 add, 1 change, 0 delete, 0 replace)</summary>

```diff
+aws:s3:Bucket logs-2026
~aws:iam:Role app-role
```

</details>

---

### api/staging · staging · ✅ ready

---

### worker/prod · prod · ✅ ready

<details><summary>Full plan output</summary>

```
pulumi preview output here
line two
```

</details>

