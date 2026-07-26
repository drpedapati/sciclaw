# sciClaw Next Public Release Draft

Assume the next public release will be `v0.2.4` unless the tag changes.

Keep the tone aligned with prior announcement files:
- short opening line
- concrete feature list
- problem/benefit framing
- one clear install/upgrade path
- no hype language that overclaims

Do not show real PHI, API keys, email addresses, bot tokens, or private channel names in screenshots.

---

## Tweet 1 - Main Release Announcement

sciClaw's next public release is the biggest workflow upgrade we've shipped so far.

New in this release:
- Discord background jobs with queue-aware status cards
- explicit `/btw` read-only side questions
- real Discord slash commands for `/btw` and `/skill`
- Claude.ai OAuth support for Anthropic oat-token users
- stronger built-in PubMed and weather tooling

The main change is operational: sciClaw behaves more like a visible team agent in a real workspace, and less like a brittle bot hidden behind one blocking request at a time.

Release notes: https://github.com/drpedapati/sciclaw/releases

---

## Tweet 2 - Queueing And Side Questions

Long requests should not freeze the room.

sciClaw now handles that more cleanly:
- one main job runs per workspace
- the next one queues instead of being silently downgraded
- `/btw` lets you ask a quick read-only side question without disrupting the main job

That makes the bot more usable in real team rooms.

---

## Tweet 3 - Discord UX

Discord now has real sciClaw commands instead of fake slash-like text conventions.

Current builds support:
- `/btw` for read-only side questions
- `/skill` with workspace-aware autocomplete
- queue-aware progress cards for long-running work

That is a cleaner product surface than forcing users to memorize message prefixes.

---

## Tweet 4 - Claude OAuth And Research Tools

Anthropic OAuth users no longer need to fall back to a Console API key.

sciClaw now routes Claude.ai oat tokens through a dedicated bridge and keeps the same sciClaw tool loop on top.

Research workflows also got stronger:
- typed `pubmed_search` and `pubmed_fetch`
- typed `weather_forecast`
- better guardrails against bad web-tool choices

---

## Tweet 5 - Upgrade / Try It

If you already use sciClaw, update and open the app.

If you are new, install from Homebrew and do the rest from the sciClaw app in your terminal.

Release notes: https://github.com/drpedapati/sciclaw/releases

Install guide: https://sciclaw.dev/docs.html

---

## Screenshot Rules

- Use synthetic or public data only
- No real PHI
- No real Discord/Telegram private channel names
- No API keys, email addresses, or bot tokens
- Prefer short workspace paths
- Show the queue/job-card or slash-command UX clearly if possible

## Phrases To Avoid

- `game changer`
- `revolutionary`
- `perfect`
- `production-ready for every machine`
- `just works` unless the exact scope is narrow and defensible
