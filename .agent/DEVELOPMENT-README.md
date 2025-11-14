# Navigator Development Guide

**Project**: mcp-stdio-proxy
**Tech Stack**: Go 1.24.4
**Initialized**: 2025-11-14

## Overview

This is the Navigator documentation structure for the mcp-stdio-proxy project. Navigator helps organize development workflows, track tasks, and maintain project knowledge.

## Directory Structure

```
.agent/
├── DEVELOPMENT-README.md      # This file - Navigator guide
├── .nav-config.json          # Navigator configuration
├── tasks/                    # Implementation plans and task tracking
├── system/                   # Architecture documentation
└── sops/                     # Standard Operating Procedures
    ├── integrations/        # Integration guides
    ├── debugging/           # Debugging procedures
    ├── development/         # Development workflows
    └── deployment/          # Deployment procedures
```

## Quick Start

### Starting a Session

```bash
# Load Navigator context and project status
"Start my Navigator session"
```

### Creating Tasks

```bash
# Document a new feature implementation
"Create a task for [feature name]"
```

### Creating SOPs

```bash
# Document a procedure you've solved
"Create SOP for [procedure name]"
```

### Context Management

```bash
# Save current progress before break
"Create a checkpoint"

# Clear context while preserving knowledge
"Compact context"
```

## Navigator Workflow

### 1. **Session Start** (`nav-start`)
- Loads CLAUDE.md project context
- Reviews recent tasks and SOPs
- Checks project status
- Provides session summary

### 2. **Task Management** (`nav-task`)
- Create: Document new features/bugs
- Update: Track implementation progress
- Archive: Mark completed work
- Review: Check task status

### 3. **Knowledge Capture** (`nav-sop`)
- Document solved problems
- Create reusable procedures
- Build institutional knowledge
- Prevent repeated problem-solving

### 4. **Context Optimization**
- **Markers** (`nav-marker`): Save progress checkpoints
- **Compact** (`nav-compact`): Clear context while preserving knowledge
- **Stats** (`nav-stats`): View efficiency metrics

## Configuration

Edit `.agent/.nav-config.json` to customize Navigator behavior:

```json
{
  "version": "3.1.0",
  "project_name": "mcp-stdio-proxy",
  "tech_stack": "Go 1.24.4",
  "project_management": "none",     // Options: "jira", "linear", "github", "none"
  "task_prefix": "TASK",            // Prefix for task files
  "team_chat": "none",              // Options: "slack", "discord", "none"
  "auto_load_navigator": true,      // Auto-load on session start
  "compact_strategy": "conservative" // Options: "conservative", "aggressive"
}
```

## Best Practices

### Task Documentation
- Create tasks before starting implementation
- Update progress regularly
- Archive completed tasks
- Link related tasks

### SOP Creation
- Document after solving novel issues
- Focus on "why" not just "how"
- Include context and decision rationale
- Keep procedures actionable

### Context Management
- Create markers before risky changes
- Compact context when approaching limits
- Review stats to optimize workflow
- Use checkpoints before breaks

## Integration Examples

### With Project Management

If using Jira/Linear/GitHub Issues:
- Link Navigator tasks to external issues
- Track implementation status
- Sync completion states

### With Team Chat

If using Slack/Discord:
- Share context markers with team
- Post SOP updates to channels
- Notify on task completions

## Troubleshooting

### "Navigator not initialized"
Run: "Initialize Navigator in this project"

### "Task directory not found"
Ensure `.agent/tasks/` exists. Re-run initialization if needed.

### "Context too large"
Use `nav-compact` to clear context while preserving knowledge.

## Next Steps

1. **Start your first session**: "Start my Navigator session"
2. **Review CLAUDE.md**: Check project-specific instructions
4. **Optional**: Configure project management integration

---

**Navigator Version**: 3.1.0
**Last Updated**: 2025-11-14
