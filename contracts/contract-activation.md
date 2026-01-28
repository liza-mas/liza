# Activation of the Contract for Pairing Agents

Check [Genesis](../README.md#genesis) for the features.

**WARNING**: Gemini and Mistral are not able to fully comply with the contract.
It's not possible to make them comply strictly with instructions. They are not recommended models for Liza.
Prefer Claude or Codex.

## Central Config

Create symlinks:
```bash
LIZA_DIR=~/Workspace/liza
mkdir -p ~/.liza
cd ~/.liza
ln -s $LIZA_DIR/contracts/CORE.md
ln -s $LIZA_DIR/contracts/PAIRING_MODE.md
ln -s $LIZA_DIR/contracts/MULTI_AGENT_MODE.md
ln -s $LIZA_DIR/contracts/AGENT_TOOLS.md
ln -s $LIZA_DIR/contracts/COLLABORATION_CONTINUITY.md
ln -s $LIZA_DIR/skills
ln -s $LIZA_DIR/scripts
ln -s $LIZA_DIR/specs
```

## Claude

Create symlinks:
```bash
cd ~/.claude
ln -s ~/.liza/CORE.md CLAUDE.md
mkdir -p skills
for i in ~/.liza/skills/* ; do ln -s "$i" skills/`basename "$i"` ; done
```

The contract is followed more strictly if the symlink is created at every repo root:
```bash
cd <REPO_ROOT>
ln -s ~/.liza/CORE.md CLAUDE.md
```

In `~/.claude/settings.json`, configure global permissions for tools used across all projects:

```json
{
  "additionalDirectories": [ "~/.liza"],
  "permissions": {
    "defaultMode": "acceptEdits",
    "allow": [
      "Read(~/.claude/**)",
      "Read(~/.liza/**)",

      "Skill(adr-backfill)",
      "Skill(code-cleaning)",
      "Skill(code-review)",
      "Skill(debugging)",
      "Skill(feynman)",
      "Skill(generic-subagent)",
      "Skill(software-architecture-review)",
      "Skill(spec-review)",
      "Skill(systemic-thinking)",
      "Skill(testing)",

      "mcp__Ref__ref_search_documentation",
      "mcp__Ref__ref_read_url",
      "mcp__perplexity__perplexity_ask",
      "mcp__deepwiki__read_wiki_structure",
      "mcp__deepwiki__read_wiki_contents",
      "mcp__deepwiki__ask_question",
      "mcp__context7__resolve-library-id",
      "mcp__context7__query-docs",
      "mcp__fetch__fetch",
      "mcp__sequential-thinking-tools__sequentialthinking_tools",

      "mcp__filesystem__read_file",
      "mcp__filesystem__read_text_file",
      "mcp__filesystem__read_media_file",
      "mcp__filesystem__read_multiple_files",
      "mcp__filesystem__list_directory",
      "mcp__filesystem__list_directory_with_sizes",
      "mcp__filesystem__directory_tree",
      "mcp__filesystem__search_files",
      "mcp__filesystem__get_file_info",
      "mcp__filesystem__list_allowed_directories",

      "mcp__jetbrains__list_directory_tree",
      "mcp__jetbrains__get_run_configurations",
      "mcp__jetbrains__get_file_problems",
      "mcp__jetbrains__get_project_dependencies",
      "mcp__jetbrains__get_project_modules",
      "mcp__jetbrains__find_files_by_glob",
      "mcp__jetbrains__find_files_by_name_keyword",
      "mcp__jetbrains__get_all_open_file_paths",
      "mcp__jetbrains__get_file_text_by_path",
      "mcp__jetbrains__search_in_files_by_regex",
      "mcp__jetbrains__search_in_files_by_text",
      "mcp__jetbrains__get_symbol_info",
      "mcp__jetbrains__get_repositories",
      "mcp__jetbrains__open_file_in_editor",
      "mcp__jetbrains__execute_run_configuration",
      "mcp__jetbrains__create_new_file",
      "mcp__jetbrains__replace_text_in_file",
      "mcp__jetbrains__reformat_file",
      "mcp__jetbrains__rename_refactoring",
      "mcp__jetbrains__execute_terminal_command",
      "mcp__jetbrains__runNotebookCell",

      "mcp__morph-mcp__edit_file",
      "mcp__morph-mcp__warpgrep_codebase_search",

      "mcp__postgres__query",

      "WebFetch",
      "WebSearch",
      "LSP",

      "Bash(~/.liza/scripts/*)",
      "Bash(curl:*)",
      "Bash(wget:*)",
      "Bash(jq:*)",
      "Bash(yq:*)",
      "Bash(sort:*)",
      "Bash(uniq:*)",
      "Bash(cut:*)",
      "Bash(tr:*)",
      "Bash(diff:*)",
      "Bash(realpath:*)",
      "Bash(dirname:*)",
      "Bash(basename:*)",
      "Bash(which:*)",
      "Bash(file:*)",
      "Bash(tree:*)",
      "Bash(env:*)",
      "Bash(printenv:*)",
      "Bash(gh:*)",
      "Bash(git commit:*)",
      "Bash(git status:*)",
      "Bash(git diff:*)",
      "Bash(git log:*)",
      "Bash(git show:*)",
      "Bash(git branch:*)",
      "Bash(git blame:*)",
      "Bash(git ls-files:*)",
      "Bash(git grep:*)",
      "Bash(pre-commit:*)",
      "Bash(python:*)",
      "Bash(python3:*)",
      "Bash(pytest:*)",
      "Bash(shellcheck:*)",
      "Bash(bash:*)",
      "Bash(ls:*)",
      "Bash(cat:*)",
      "Bash(head:*)",
      "Bash(tail:*)",
      "Bash(wc:*)",
      "Bash(date:*)",
      "Bash(find:*)",
      "Bash(grep:*)"
    ]
  }
}
```

**Permission categories:**
- `"defaultMode": "acceptEdits"` — Required for Liza agents to work headless (preferred to `"bypassPermissions"` aka YOLO mode)
- `Read(~/.liza/**)` — Access to contract files
- `Bash(~/.liza/scripts/*)` — Execution of Liza scripts
- `Skill(...)` — Custom skills from `~/.liza/skills/`
- `mcp__...` — Your configured MCP tools
- `WebFetch/WebSearch/LSP` — Built-in Claude tools for web and code navigation
- Other `Bash(...)` — Safe read-only shell commands (no package managers)

This enables auto-accept mode for headless agents. If agents get blocked on additional tools, add them to your global settings. Refer to "Debug a stuck agent interactively" in [DEMO.md](../docs/DEMO.md#troubleshooting) to identify blocking commands.

Verification:
- Run `claude`
- Prompt `hello`

## Codex

Create symlinks:
```bash
mkdir -p ~/.codex/skills
cd ~/.codex
ln -s ~/.liza/CORE.md AGENTS.md
for i in ~/.liza/skills/* ; do ln -s "$i" skills/`basename "$i"` ; done
```

The contract is followed more strictly if the symlink is created at every repo root:
```bash
cd <REPO_ROOT>
ln -s ~/.liza/CORE.md AGENTS.md
```

Edit ~/.codex/config.toml:

```toml
approval_policy = "on-failure"
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = true
writable_roots = ["/home/<USER>/.codex",  "/home/<USER>/.liza", "/home/<USER>/.pyenv/shims", "/home/<USER>/.cache"]

[mcp_servers.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/home/<USER>/.claude", "/home/<USER>/.codex", "/home/<USER>/Workspace", "/home/<USER>/.liza", ]
```

## Gemini

Create symlinks:
```bash
mkdir -p ~/.gemini/skills
cd ~/.gemini
ln -s ~/.liza/CORE.md GEMINI.md
for i in ~/.liza/skills/* ; do ln -s "$i" skills/`basename "$i"` ; done
```

The contract is followed more strictly if the symlink is created at every repo root:
```bash
cd <REPO_ROOT>
ln -s ~/.liza/CORE.md GEMINI.md
```

## Mistral

Symlink the contract as instructions and add skills:
```bash
mkdir -p ~/.vibe/prompts ~/.vibe/skills
cd ~/.vibe
ln -s ~/.liza/CORE.md prompts/liza.md
for i in ~/.liza/skills/* ; do ln -s "$i" skills/`basename "$i"` ; done
```

Modify `~/.vibe/config.toml`:
- Add `system_prompt_id = "liza"` (replace `system_prompt_id = "cli"` with)
- Add MCP filesystem server (replace `mcp_servers = []` with):
```toml
[[mcp_servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/home/<USER>/.vibe", "/home/<USER>/Workspace", "/home/<USER>/.liza"]
```

Verification:
- Run `vibe`
- Prompt `Hello. You MUST follow the contract.` ("hello" is not enough for Gemini and Mistral)
