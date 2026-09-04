---
name: comment-style-generic
description: "User prefers generic comments describing what each command/function does, not specific usecase detail"
metadata: 
  node_type: memory
  type: feedback
---

User wants code/config comments (Makefile targets, etc.) to stay generic — describe what each command does at a high level, not explain a specific usecase or scenario behind it.

**Why:** Detailed usecase comments rot fast and clutter; the command's purpose should be self-evident.

**How to apply:** Write one short line per target/function saying what it does (e.g. "thêm host entries vào /etc/hosts nếu chưa có"). Drop narrative context like "for local SSO testing to isolate cookies".
