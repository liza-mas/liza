# Cluster 0119 - Explicit Unattended Setup and Initialization

## Status

Not selected for the eight-record backfill approved on 2026-09-06. Analysis retained for possible future use; no ADR generated.

## Commit Set

- `f6925e10` — feat(cli): add yes auto-confirm for setup and init
- `255882c3` — fix(cli): auto-confirm bash-policy init prompts

Earliest author timestamp: 2026-07-08T16:20:47+02:00. Decision implementation range: 2026-07-08 to 2026-07-08.

## Reconstructed Decision

Make setup and initialization usable unattended through explicit --yes, including delegated policy setup prompts.

Thread AutoConfirm through setup/init and provide bounded scripted yes input to bash-policy subprocesses.

**Rationale provenance:** Commit messages identify unattended setup as the trigger; alternatives and accepted approval tradeoffs are unconfirmed.

## Evidence

- f6925e10: cmd/liza/cmd_init.go adds explicit --yes and detected-provider selection; internal/commands/setup.go and init.go propagate AutoConfirm.
- 255882c3: internal/bash-policy-cli/bash_policy_cli.go adds initHooksStdin and 16 scripted yes responses per subprocess.

Source reads were targeted to the named commit diffs and current sections; they do not establish the human's historical alternatives or acceptance of tradeoffs.

## Related Decisions

ADR-0018, ADR-0050, ADR-0091. Existing IDs stay unchanged; this candidate is numbered chronologically within the post-0097 backfill batch.

## Human Context Needed

- Is explicit unattended setup a durable architecture choice or ordinary CLI capability?
- Were alternatives considered beyond interactive setup, and was bounded scripted subprocess input an accepted interim constraint?
- Correct the reconstructed trigger/rationale and name any rejected alternatives, accepted limitations, or additional related decisions.
