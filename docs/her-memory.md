# Her's memory

> **2026-11-20: Her has no action approvals.** Her runs every action without asking: no permission modes, no approval window, nothing for the owner to approve before a step. Where this document still speaks of an approval of an action, a step waiting for the owner's approval, or full access, it describes what was and no longer is. Secure cards are not approvals and stay.

Status: the base (§3–§7) built 2026-11-11; knowledge entries (§10) built
2026-11-20. What §9 lists is not built. It builds on [her-server.md](her-server.md): an assistant has "its
own conversation, memory, folder" (§3 there), and a Her Server holds the
only copy (§7 there).

## 1. Goal, and what is left out

An assistant's conversation is summarized as it grows, and the owner can
clear it. Without memory, what the owner said about themselves and how they
want things done lasts only as long as a summary happens to carry it.

Memory is what the assistant keeps so that the owner never says the same
thing twice: who they are, how they want things done, what was decided on
things that go on, and where things are.

Three choices shape it:

- **Files.** Memory is Markdown files in the assistant's directory. The
  assistant writes them with the file tools it has anyway, the owner can
  read and change them, and there is no database, no index and no second
  model.
- **The model decides when to write.** The assistant's prompt says what
  memory is for, when to write and what to leave out (§5). Nothing extracts
  memories from the conversation behind its back.
- **In the system prompt.** What is always known is the last part of the
  assistant's system prompt, where the model follows it best (§4).

Left out: memory for sessions that are not an assistant's (coding sessions,
the `her` command line), memory shared between assistants or between Her
Servers, and everything in §9.

§10 adds a second kind of note beside `MEMORY.md`: knowledge entries, each
with a trigger, which reach the model only when the owner's words hit the
trigger, and which Her proposes and the owner saves.

## 2. What other agents do

Looked at before building (2026-11): Hermes Agent, OpenClaw, Letta, Claude
Code, ChatGPT, Codex and Anthropic's "The components of agent memory".

- Most keep a small part always in context, files read on demand, and the
  raw conversations to search. Vector stores and knowledge graphs have no
  neutral evidence in their favour at the size of one person's notes.
- Most put the always-known part in the system prompt, as a copy taken when
  a session starts. Hermes Agent says why it never changes it within a
  session: the prompt cache.
- Telling a capable model what memory is for works better than fixed rules;
  a weaker model needs to be told when to read and write.
- Memory written from untrusted content is an attack that has happened to
  other products: an instruction in a web page ended up in long-term memory.

An assistant's conversation never ends, so "a copy taken when the session
starts" does not carry over as it is. §4.2 is what replaces it.

## 3. The files

### 3.1 Where

```
~/.her/bots/<id>/
  bot.json
  files/          the assistant's working folder
  memory/
    MEMORY.md     always in the system prompt
    <subject>.md  pages, read when needed
```

`memory/` is beside the working folder, not in it:

- Sub-agents and routine runs share `files/` and write there freely. Beside
  it, their writes are outside their workspace and inside her's data
  directory; the tool policy refuses their writes to a bot's memory (§6).
- Work in the folder ("tidy up my files") cannot sweep the notes away.
- A patrol routine's notes are beside them too, in `patrol/<routine>/`, the
  one place its runs write ([her-patrol.md](her-patrol.md)).

What follows from the place, with no code of its own:

- `bots/clear` moves the conversation to the trash and leaves the
  assistant's directory: memory stays, and the next conversation starts
  with it.
- `bots/delete` moves the whole directory to `trash/bots/`: memory goes
  with the assistant.
- Each assistant has its own memory, and so has each Her Server.

### 3.2 `MEMORY.md`

One fact per line, under a few headings, in the owner's language. The kernel
reads the file as text and knows nothing of its headings.

```markdown
## About the user
- The weekly report is due on Friday afternoon and goes to the team's group. (2026-11-11)
- Lives in Shanghai from 2026-12; Hangzhou before that. (2026-11-20)

## How they want things done
- Replies are always in Chinese, the summary at the end of a task too. (2026-11-11)

## Going on
- Preparing the December launch; state and open points are in launch-dec.md: read it when the launch comes up. (2026-11-18)

## Where things are
- Scanned contracts are in ~/Documents/contracts, one folder a year. (2026-11-12)
```

Each line is a full sentence that still makes sense a year later, and ends
with the date it was written: the date says how old a fact is, and which of
two lines is newer.

### 3.3 Pages

A subject that needs more than a line or two (a project, a person, how a
recurring job is done) gets a page in the same folder, and one line in
`MEMORY.md` that names the page and says when to read it. Pages are not in
context: the assistant reads one when the request touches it.

### 3.4 The limit

Only the first 8 KB of `MEMORY.md` are loaded (`bot_memory::LIMIT`), cut at
the last whole line. The loaded text then ends with a line that says how
large the file is, how much was loaded, and to shorten it in that turn. So
the file is kept small by the assistant, and the kernel never rewrites it.

8 KB is a starting value: larger than Hermes Agent's (about 1,300 tokens),
smaller than Claude Code's (25 KB), since every request carries it.

## 4. How it reaches the model

### 4.1 The last part of the system prompt

An assistant's system prompt ends with its memory, after the MCP part:

```
<memory path="/Users/…/.her/bots/<id>/memory/MEMORY.md">
…the file, or (empty)…
</memory>
```

An assistant with nothing written yet gets it too: the path is where to
write. The prompt's own text (`bot.md`, §5) says what the block is.

The system prompt is where a model follows content best, and it stays
first however long the conversation gets. A block in a user message is read
as reference, and moves further from the latest turn with every message.

Sub-agents and routine runs get no memory in their prompts: the assistant
puts into a task the lines that matter for it.

### 4.2 When the prompt takes a new copy

The block is a copy of the file. Changing it changes the system prompt, and
a changed system prompt makes the provider read the whole conversation again
at the full price. So the prompt takes the file as it is now only when that
costs nothing:

1. **A conversation starts.** The prompt is new anyway.
2. **Right after a compaction.** The context is new anyway. The summary
   itself is still asked for with the prompt as it was.
3. **A task starts and the model last answered at least 10 minutes ago**
   (`bot_memory::QUIET`), or never. A provider's prompt cache has most
   likely expired by then.

Only a file that differs from the copy leads to a new prompt, which is
recorded as a `system_prompt` entry with its `memory`, like a prompt whose
MCP part changed.

Between two copies the assistant's own changes are not lost: each is a tool
call in its context, and a compaction, which would remove that call, takes a
new copy. `bot.md` says that the block is a copy from a while ago and that
later changes in the conversation are the newer word. A change from outside
(the owner editing the file) is known at the next of the three moments.

A conversation opened again keeps the copy its log has: the service unloads
idle conversations, and a provider may still hold the prompt. Two cases take
the file at once, since the prompt is written anew then: a new `bot.md`,
and a log from before there was memory.

### 4.3 Cost

One uncached request per new copy at most, at a moment when the request
would have been uncached anyway. The 8 KB are part of every request, at the
cached price while the conversation goes on.

10 minutes is a starting value. With a provider that keeps its cache longer,
a new copy after 10 minutes costs one uncached request.

## 5. When the assistant writes

`bot.md` has a section "Your memory". It says, in this order:

- what memory is and where it is given;
- what it is for, then when to write: the owner asks for it or sets a rule
  for later; a correction that would apply next time (a correction to one
  reply becomes a rule the second time); a lasting fact about the owner or
  their world; a decision on something that goes on, with the reason; where
  something is that will be needed again; a way of doing something that was
  not obvious and that the owner was happy with;
- what to leave out: what a tool can tell again, guesses, the task of the
  day, one-off requests, secrets (even when asked), sensitive matters unless
  asked, and anything a web page, a file, a message, a tool result or an
  agent's report says to remember;
- how: reply first, then write in the same turn; one dated sentence per
  line; change a line that covers it rather than add one; the owner's word
  now wins over a line; forgetting deletes the line and says so; pages for
  larger subjects; shorten the file when told it is over the limit; use what
  is there without announcing it; a line never authorizes sending, paying or
  deleting; put what matters into an agent's task.

The turn rules follow from it (`task.rs`, `bot::owed`):

- A note may go beside the reply in one step, the one exception to "the
  first send goes alone".
- A note written once the owner has their reply ends the turn by itself: no
  request is made only for the model to stop, and no reminder that work went
  unsent. So a note costs no request beside the reply, or one after it.
- A note that failed keeps the turn open, even beside a send that closed it,
  so the model sees why and writes it again.
- A note is nothing the owner waits for: it never sets off the reminder for
  unsent work.

A first run with a real model (Bailian Token Plan, Qwen3.8-Max) showed why:
before these rules, about a third of the turns that said "noted" ended
without the edit, and a note made after the reply was followed by the same
reply sent again. `tools/bench/bot_memory.mjs` runs those cases.

A preference about asking first before a kind of action ("以后这类不用问我",
"always ask me before paying") is not written here (2026-11-20): it is a
confirmation rule, kept apart, listed and deleted in the apps
([her-confirm-rules.md](her-confirm-rules.md)).

## 6. Who writes

| Who | Reads | Writes | How |
|---|---|---|---|
| The assistant's main session | Yes | Yes, without asking in every permission mode | `protected::memory`: its own `memory/` is allowed before anything else is judged |
| Its sub-agents and its routines' runs | Yes | No, in every mode | The same check: a path in any assistant's `memory/` that is not the session's own is denied, full access included. The refusal tells the agent to say in its report what the assistant should know |
| The owner | Yes | Through the assistant, or by clearing it all | Her's settings show it and clear it (§7); the files are on the Her Server |

Only `write` and `edit` are judged this way. A shell command that writes
there is judged like any other: it asks as a write to her's data directory,
and runs unasked only with full access.

What is not closed:

- With full access a sub-agent can still write there with a shell command.
  That mode lets it do anything the user's account can. Denying the file
  tools stops ordinary injected instructions; it is not a boundary.
- The assistant itself may note something harmful after reading a poisoned
  report. The rule in §5 is the only guard, and a rule in a prompt is not
  certain. This is why the owner must be able to see and remove what is kept
  (§7).

## 7. In the apps

Her's settings, on the Mac and the iPhone, have a Memory section: one row
that opens a page of its own, and says "Nothing yet" when nothing is kept,
and a note on what Her keeps and that it stays when the chat is cleared.
Clear Memory sits beside Clear Chat.

- The page shows `MEMORY.md` as written, rendered as Markdown the way the
  conversation renders it, then the other files of the folder, which open
  as a file Her sent does. It does not parse the file into rows: what the
  memory holds and how it is laid out is the assistant's prompt's business,
  and may change, so the apps show whatever is there.
- Nothing of `MEMORY.md` is edited on the page. A line is changed by
  telling Her. Knowledge entries and Her's proposals are the exception
  (§10.5): they are saved, changed and deleted there.
- Clear Memory asks first, then moves the folder to `trash/memory/` and
  makes the conversation's prompt drop it at once, not at the next free
  moment of §4.2. The chat stays.
- Clearing the chat says that what Her remembers stays.

Two methods carry it, which aided lets the owner's devices call:
`bots/memory` (the file as written, its path, the other files with when each
changed) and `bots/memory_clear`.

## 8. Tests

- Kernel (`her-core/tests/memory.rs`, scripted model, real tools, real
  logs): the block at a conversation's start, with and without a file; a
  change leaves the prompt alone while the conversation goes on; a
  conversation opened again at once keeps its copy; a new copy after a quiet
  time and after a compaction, each recorded once; a log from before memory;
  the limit; writing without asking in manual mode; a routine's run denied
  with full access.
- The write rule alone: `policy::tests::a_bots_memory_is_written_by_the_bot_alone`.
- The service (`her-cli/tests/e2e_bots.rs`, `her serve` and the mock model's
  `remember` and `recall`): a note, a cleared conversation that starts with
  it, and the notes in the trash with a deleted assistant.
- End to end (`e2e/her_memory_test.go`, `TestHerMemory`): the same through
  the owner's Mac, the relay and the tunnel to a Her Server, reading and
  clearing the memory as the apps do.
- The apps: the desktop's Her test (`apps/desktop/e2e/her.spec.ts`, run by
  `TestDesktopHer`) and the iPhone's (`HerUITests.herMemory`, run by
  `TestIPhone`): the row says nothing is kept, Her notes a line, the page
  shows it rendered, Clear Memory asks and empties it.

## 9. Not built

- **A notice in the conversation** when the assistant notes something, a
  quiet line like the one for a changed routine that opens the change.
- **Erase Everything** does not reach `~/.her` today, memory included.
- **Routine notes**: what a routine learns about its own job (what it saw
  last time, how the job is done) belongs to the routine, not to the
  assistant's memory. What a run finds out about the owner goes in its
  report; the assistant tells the owner, and notes it once the owner has
  taken it up.
- **Searching earlier conversation**, compacted parts included.
- **Tidying**: a scheduled pass that checks the notes against the
  conversation, merges repeats and marks what is out of date.
- **Evaluation across models**: `tools/bench/bot_memory.mjs` measures one
  model at a time (when it writes, what it leaves out, recall after a
  clear). Only Bailian Token Plan has been run; whether 8 KB and 10 minutes
  are the right values is not measured.

## 10. Knowledge entries

Borrowed from Devin's Knowledge (the borrow list's item 12): a note
carries a trigger, a description of when it matters, and is recalled when
that comes up instead of being in every request. Her proposes notes; the
owner saves them.

### 10.1 What was there before

- `MEMORY.md` was the only thing in context, all of it (up to 8 KB) in
  every request, as the end of the system prompt (§4).
- Pages (§3.3) were already recalled by condition, but by the model alone:
  a line of `MEMORY.md` said "read launch-dec.md when the launch comes up",
  and that line itself was always there.
- The apps showed the memory and cleared it; nothing could be changed or
  deleted one note at a time, and nothing waited for the owner's yes.

Both stay exactly as they were: a `MEMORY.md` with no entries beside it
gives the same prompt, byte for byte, and a page is still read when needed.

### 10.2 The files

```
memory/
  MEMORY.md                 always in the prompt (§3.2), as before
  <subject>.md              pages (§3.3), as before
  knowledge/<name>.md       entries: recalled when their trigger is hit
  proposals/<id>.json       what Her proposed, waiting for the owner
```

An entry is Markdown with one line of front matter:

```markdown
---
trigger: 周报; weekly report
---
The weekly report goes to the team's group on Friday afternoon, as a PDF. (2026-11-20)
```

- `name`: letters, digits, `-` and `_` in any script, at most 64; it is
  the file name.
- `trigger`: one line, at most 300 bytes: phrases separated by `;` (`,`
  `、` `|` and full-width forms split too). A phrase of one byte (a single
  Latin letter) never counts; a single Chinese character does.
- The text: at most 8 KB (`knowledge::TEXT_LIMIT`), as `MEMORY.md`.
- A file in `knowledge/` without a trigger (written by hand) is listed but
  never recalled by itself.

Clearing the memory and deleting the assistant take entries and proposals
with the folder, as before.

### 10.3 How an entry reaches the model

Two ways, both cheap:

1. **Recalled by the kernel when the trigger is hit.** Before each request,
   the owner's messages since the model last answered are compared with
   every entry's phrases (case-insensitive substring). Each entry hit is
   recorded as a `recall` entry in the session log and read by the model as
   a user message after the owner's:

   ```
   <recall entry="weekly-report">
   From your knowledge, because the user's words match this entry's trigger. …

   The weekly report goes to …
   </recall>
   ```

   At most 5 entries a request (`RECALL_LIMIT`). An entry the context
   already holds with the same text (since the last compaction) is not
   given again; a changed one is.
2. **Listed in the prompt, read by the model.** After `<memory>`, the
   system prompt lists each entry by name and trigger, never its text:

   ```
   <knowledge path="…/memory/knowledge">
   - launch: 发布会; launch
   - weekly-report: 周报; weekly report
   </knowledge>
   ```

   When a request touches an entry in other words than its phrases, the
   model reads `<name>.md` itself (`bot.md` says so). The list is part of
   the memory block and follows §4.2: it changes only at the free moments.
   With no entries there is no list.

Routine reports, sub-agents' reports and cards do not recall: only the
owner's words. Sub-agents and routine runs get neither the list nor the
recalls, as with `MEMORY.md` (§4.1).

### 10.4 Why this way: cost and what was left out

| Way | Cost per request | Recall | Why or why not |
|---|---|---|---|
| Phrases, matched by the kernel (chosen) | Nothing until hit; then the text, once while the context holds it, in a user message: the cached system prompt is untouched | Exact phrases only | Deterministic and testable, no service, no model call, works for Chinese without a tokenizer |
| A list in the prompt, the model reads (chosen, beside it) | One line per entry, cached | Paraphrases too, when the model judges it | Covers what phrases miss; one `read` call when used |
| Embeddings | An embedding call per message | Paraphrases | A new service or a local model: refused by the ticket and §1 ("no index, no second model") |
| A second model choosing | A model call per message | Best | Doubles latency and cost for every message |

- Putting a recalled text in the system prompt was ruled out: a changed
  system prompt costs a full uncached request (§4.2). A user message after
  the owner's costs only its own tokens.
- Phrases miss what is said in other words; the list in the prompt is the
  fallback. Phrases also hit what merely contains them ("周报" in "周报表");
  the recall message tells the model to use it only if it bears on the
  request.
- 5 entries a request and 8 KB each bound a hit at 40 KB; in practice an
  entry is a few lines.

### 10.5 Proposals: Her proposes, the owner saves

- Her's own conversation has `propose_memory {name, trigger, text}`. It
  writes `proposals/<id>.json` and nothing else; the result tells the model
  the proposal is not in its memory until the owner saves it.
- `bot.md` says when: feedback on how she did something, a settled way of
  handling a recurring job, project or subject that matters only when it
  comes up, and at the end of a piece of work when she learned such a
  thing. One proposal per subject, said in her reply; never again what the
  owner declined. What matters on most turns still goes into `MEMORY.md`.
- For the turn rules (§5) a proposal counts as a note: it may go beside the
  reply, and a step of nothing but notes ends the turn once the owner has
  their reply.
- **A proposal has no effect until the owner saves it**: it is not listed,
  not recalled, not readable as an entry. Saving moves it to
  `knowledge/<name>.md` (replacing an entry of that name, which the page
  says); declining deletes it. Either way it is answered once.
- **No session's file tools write `knowledge/` or `proposals/`**, the bot's
  own main session included (`protected::owners_entries`, refused with a
  word pointing to `propose_memory`). The rest of the bot's memory is
  written as §6 says. A shell command with full access could still write
  there, as §6 says of the whole folder.

Methods, which aided lets the owner's devices call:

| Method | Does |
|---|---|
| `bots/memory` | Also gives `entries` (name, trigger, text) and `proposals` (id, name, trigger, text, created_at, replaces) |
| `bots/memory_proposal_answer {bot_id, proposal_id, accept}` | Saves or declines a proposal |
| `bots/memory_entry_set {bot_id, name, trigger, text}` | Saves an entry, in place of one of that name |
| `bots/memory_entry_delete {bot_id, name}` | Deletes it; the conversation's prompt drops it at once, as Clear Memory does |

A saved or changed entry is recalled from the owner's next message on; the
prompt's list takes it at its next free moment.

### 10.6 In the apps

- **Desktop** (Memory page): "待确认的提议" at the top, each with its
  name, trigger and text, Save and Decline; "按需想起的条目" after the
  pages, each with Edit (the trigger and the text, in place) and Delete
  (asks first).
- **Android** (Her's Memory tab): the same proposals with Save and Decline,
  the same entries with Edit and Delete. Its Her entry talks to the same
  methods through the Go core, so nothing stood in the way.
- The proposal is not shown as a card in the conversation: secure cards
  (her-secure-cards.md) carry console calls aided checks as the console's,
  and an in-chat card for a proposal would be a new kind with its own
  renderer on each app. Her says in her reply that she proposed something
  and where to save it. A card is the natural next step.

### 10.7 Tests

- Unit: `knowledge::tests` (phrases, front matter, save, list, recall,
  delete, proposals), `policy::tests::knowledge_entries_and_proposals_are_not_written_with_file_tools`,
  her-mock's `recalled`.
- Kernel (`her-core/tests/memory.rs`, scripted model, real tools and logs):
  entries with and without triggers and a page, recalled only when hit,
  once while the context holds them, `MEMORY.md` still in the prompt; a
  proposal recalled only once saved, a declined one never; a file-tool write
  to `knowledge/` refused.
- Service (`her-cli/tests/e2e_bots.rs`, `her serve` and her-mock's
  `propose` and `recalled`): propose two, nothing recalled; save one and
  decline one; a miss, then a hit that recalls only the saved one; the
  owner's edit recalled as changed; delete; `MEMORY.md` unchanged.
- End to end (`e2e/her_memory_test.go`, `TestHerKnowledge`): the same
  through aided, the relay and the tunnel, as the apps call it.
- Desktop (`apps/desktop/e2e/her.spec.ts`, `TestDesktopHer`): Her proposes
  two, the page shows them, Save and Decline, Edit, then the recall in the
  conversation, and Delete after asking.
- Android: `HerTest.botsRoutinesAndMemory` parses entries and proposals.
