---
plan_recommended: false
---

# Production Server Sysadmin Intent

## Context

I run a web application on production and pre-prod Ubuntu VMs on OpenStack. They run Docker Compose
containers and lack resource monitoring, database backups, update automation, and alerting.

I have SSH access and management API access. Clara should do this work for me rather than merely
listing what I need to research.

**New MCP Server Required:** an `ssh` MCP server.

## Workflow

### 1. New SSH MCP Server

Build `internal/mcpserver/ssh/` with tools:
- `ssh.exec(host, command)`
- `ssh.upload(host, local_path, remote_path)`
- `ssh.download(host, remote_path, local_path)`

Hosts are defined in config.

### 2. Monitoring Intent

Check:
- CPU
- Memory
- Disk
- Container health
- Docker daemon health

Store metrics in SQLite and alert when thresholds are exceeded.

### 3. Database Backup Intent

Run scheduled DB dumps over SSH, download backups locally, compress them, and keep rolling history.

### 4. Update Management Intent

Check for:
- OS package updates
- Docker image updates

Present update actions in the TUI for review. Security updates can optionally auto-apply.

### 5. One-off Operations

Expose the SSH MCP server in TUI/tool calls for ad hoc server inspection and operations.

## Acceptance Criteria

- `ssh.exec` works against configured hosts
- Monitoring runs on schedule and stores metrics
- Disk usage alerts appear in TUI when thresholds are exceeded
- Daily backup runs and local retention cleanup work
- Update list appears in TUI with approval workflow
- SSH credentials live in config, not code
