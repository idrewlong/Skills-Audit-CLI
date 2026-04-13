# skill-mgr

**Agent skill manager** — list, audit, and uninstall AI coding agent skills installed via [skills.sh](https://skills.sh) or directly.

Works across all major agents: Claude Code, Cursor, Codex, GitHub Copilot, Cline, Amp, Windsurf.

---

## Install

```bash
# Homebrew (recommended)
brew install idrewlong/tap/skill-mgr

# Go install
go install github.com/idrewlong/skill-mgr/cmd/skill-mgr@latest

# From source
git clone https://github.com/idrewlong/skill-mgr
cd skill-mgr
make install
```

---

## Commands

### `skill-mgr list`

Inventory all installed skills across every agent directory.

```bash
skill-mgr list
skill-mgr list --audit          # include risk score per skill
skill-mgr list --agent cursor   # filter by agent
skill-mgr list --json           # machine-readable output
```

### `skill-mgr audit`

Static security analysis — scans every skill file for threats.

```bash
skill-mgr audit
skill-mgr audit --verbose            # show full findings
skill-mgr audit gsap-core --verbose  # audit one skill
skill-mgr audit --registry           # also fetch Gen/Socket/Snyk scores
```

**What it detects:**
- Prompt injection / instruction override attempts (`PI-*`)
- Credential exfiltration (`.ssh`, `.aws`, `.env` + network calls) (`EX-*`)
- Curl-pipe-bash / remote code execution (`SH-*`)
- Hardcoded C2 IP addresses (`EX-003`)
- Background process / cron scheduling (`SH-003`)
- Base64-encoded payloads / obfuscation (`OB-*`)
- Privilege escalation (`sudo`, `rm -rf`) (`PR-*`)
- Hook abuse (Claude Code auto-execution vectors) (`HK-*`)
- Social engineering language (`SE-*`)

**Risk levels:** SAFE (0) → LOW (1–15) → MEDIUM (16–35) → HIGH (36–60) → CRITICAL (61–100)

Exits with code `1` if any skill is HIGH or CRITICAL (useful in CI).

### `skill-mgr remove <name>`

Symlink-aware uninstall — removes all agent copies automatically.

```bash
skill-mgr remove gsap-core --dry-run  # preview first
skill-mgr remove gsap-core            # confirm and remove
skill-mgr remove gsap-core --force    # skip confirmation
```

### `skill-mgr check-updates`

SHA-based update detection via the skills.sh registry, with git fallback.

```bash
skill-mgr check-updates
skill-mgr check-updates frontend-design  # check one skill
```

### `skill-mgr info <name>`

Full detail on one skill: frontmatter, paths, audit results, update status.

```bash
skill-mgr info frontend-design
```

---

## How it works

`skill-mgr` scans all known agent skill directories:

| Agent | Path |
|---|---|
| Universal (skills.sh) | `~/.agents/skills/` |
| Claude Code | `~/.claude/skills/` |
| Cursor | `~/.cursor/skills/` |
| Codex | `~/.codex/skills/` |
| GitHub Copilot | `~/.github-copilot/skills/` |
| Cline | `~/.cline/skills/` |
| Amp | `~/.amp/skills/` |
| Windsurf | `~/.windsurf/skills/` |

Skills installed universally via `npx skills add` are symlinked into each agent directory. `skill-mgr` deduplicates them and tracks all symlink targets so `remove` cleans up every copy.

---

## Zero dependencies

Pure Go stdlib — no external packages. Single static binary.

---

## License

MIT
