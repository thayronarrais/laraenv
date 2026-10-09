# Terminal workspace

The terminal workspace combines running shells and agent sessions with a navigator and document editor. The navigator has **Agents**, **Files** and **Git** tabs. Use its collapse control to hide it, the main navigation control to switch to an icon rail, and **Editor** to show or hide the document area. Hidden panels release their space; running terminals remain mounted.

## Files and Markdown

Choose a project or focus a terminal/agent session to select its effective checkout. Files lists directories before files, including when a large directory requires another page. Expand a directory or enter a relative path to open a document.

Text files open in the editor. Markdown supports source and rendered preview; supported images open in a preview. Tabs identify their project or worktree, so two files named `README.md` in different working copies remain distinct.

Save writes only within the selected workspace and checks the disk revision first. An external change opens a conflict decision rather than silently overwriting it. Closing a dirty document, quitting, or applying an update requires a save/discard/cancel decision. A document can remain open after its terminal exits.

## Git changes

Git follows the selected checkout. **Staged changes** and **Working tree changes** each have a folder tree that can be expanded or collapsed. Filter paths, review a file's diff, and stage or unstage files or the visible selected group using the labelled controls.

Write a commit message and select **Commit**. A commit includes every staged change in that checkout's index; the search filter and checkboxes do not limit its contents. If staging touches an open dirty document, choose whether to save first, stage its disk version, or cancel.

**Push** is separate from Commit. Select the remote and destination branch and optionally set it as upstream. Running operations expose progress and cancellation; errors remain visible. Successful operations share a compact summary with expandable history.

Git changes describe the working copy, not ownership by a particular agent. When agents share a checkout, they also share its changes and index. Isolated worktrees keep those separate.
