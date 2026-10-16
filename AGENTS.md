# Development policy

This project has no actual users yet. Until the maintainer explicitly announces adoption:

- Prefer the current design over backward compatibility. Do not add database migrations, old-format readers, legacy API adapters or fallback execution paths solely to preserve previous project behavior.
- Remove obsolete code and tests with the contract they covered. Keep tests for current behavior, resource permissions, concurrency, persistence, recovery and atomic publication.
- Accept only the current database and share-package formats. Reject unsupported formats clearly; do not automatically delete existing data files.
- Profile revisions and restoration of current configuration are product features, not legacy compatibility.

# Desktop product policy

- The desktop App is a consumer product for ordinary users. Its primary experience is conversation and completing the user's work. A user should only need to learn how to connect a model, without learning the Agent architecture.
- Provide a ready-to-use default assistant and a direct New conversation action. Do not require users to create or understand Profiles, LocalContexts, components, workflows, MCP, Skills or runtime identifiers before chatting.
- Preserve the harness's customization capabilities. Present Profiles as assistants in ordinary user flows; put configuration management in Settings and runtime/component/workflow/schema controls in an explicitly named Developer settings area. The CLI remains available for developer workflows.
- Keep one Developer settings entry in App Settings. Assistant editing may link to that same page with the selected assistant, but must not duplicate its configuration panel. Keep assistant Skill/MCP enablement visible in the assistant editor; distinguish selecting installed capabilities from installing or connecting them. Template cards must state their action and show creation progress, success and errors beside the cards; handle unsaved changes before creating anything.
- Keep the default conversation screen focused on messages, assistant selection, attachments, the existing web-search toggle and necessary approvals. Put execution graphs, revisions, IDs, fingerprints, budgets and raw tool records in task details or developer views. Web search remains a conversation option, not a new Profile field or separate webpage-reading button.
- Make model connection one coherent flow: provider, model and credentials, followed by understandable consent when sending content requires authorization. Derive destinations from the actual endpoint, including custom endpoints. Simplifying consent must preserve permission boundaries, scope checks, frozen ceilings and explicit authorization; never replace them with blanket grants.
- Explain approvals by the concrete action, affected files or destinations and meaningful consequences. Explain unknown outcomes with guided verification and an option to remain unsure; do not force users to invent a successful result or automatically replay possible side effects.
- Use familiar chat interactions: readable Markdown and code, scrolling that follows new output without interrupting reading, keyboard and input-method support, clear attachment handling and native file/folder selection where needed. Keep technical limits in contextual help and actionable validation.
- Use plain user-facing names, translated states and actionable errors with optional technical details. Do not require editing JSON/config files, copying runtime IDs or using browser prompt/confirm dialogs for normal App workflows. Keep experimental limitations truthful without making runtime development notes the main product introduction.
- Protect unfinished work: preserve conversation drafts, distinguish editing an assistant from selecting it for a conversation, and guard unsaved configuration changes. Disabled or blocked actions must explain why and offer the next usable action.
- Validate ordinary-user journeys as well as runtime correctness: first launch, model connection, direct conversation, attachments, permissions, interruption recovery and conversation management. A passing build or mocked runtime test alone does not establish product usability.
