PRAGMA foreign_keys = ON;

BEGIN;

CREATE TABLE IF NOT EXISTS repo_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    github_owner TEXT NOT NULL CHECK (length(trim(github_owner)) > 0),
    github_repo TEXT NOT NULL CHECK (length(trim(github_repo)) > 0),
    default_branch TEXT NOT NULL CHECK (length(trim(default_branch)) > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sprints (
    sprint_id TEXT PRIMARY KEY CHECK (length(trim(sprint_id)) > 0),
    sequence_no INTEGER NOT NULL UNIQUE CHECK (sequence_no > 0),
    github_issue_number INTEGER NOT NULL UNIQUE CHECK (github_issue_number > 0),
    github_issue_node_id TEXT NOT NULL UNIQUE CHECK (length(trim(github_issue_node_id)) > 0),
    title TEXT NOT NULL,
    body_md TEXT NOT NULL,
    goal TEXT,
    done_when_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (
        status IN (
            'todo',
            'in_progress',
            'partially_done',
            'ready_for_sprint_pr',
            'awaiting_human',
            'sprint_pr_open',
            'sprint_reviewing',
            'merge_failed',
            'done',
            'blocked',
            'canceled'
        )
    ),
    sprint_branch TEXT,
    active_sprint_pr_number INTEGER,
    timeline_log_path TEXT NOT NULL CHECK (length(trim(timeline_log_path)) > 0),
    needs_human INTEGER NOT NULL DEFAULT 0 CHECK (needs_human IN (0, 1)),
    human_reason TEXT,
    opened_at TEXT,
    closed_at TEXT,
    last_issue_sync_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (sprint_id, github_issue_number)
);

CREATE TABLE IF NOT EXISTS tasks (
    task_id TEXT PRIMARY KEY CHECK (length(trim(task_id)) > 0),
    sprint_id TEXT NOT NULL,
    task_local_id TEXT NOT NULL CHECK (length(trim(task_local_id)) > 0),
    sequence_no INTEGER NOT NULL CHECK (sequence_no > 0),
    github_issue_number INTEGER NOT NULL UNIQUE CHECK (github_issue_number > 0),
    github_issue_node_id TEXT NOT NULL UNIQUE CHECK (length(trim(github_issue_node_id)) > 0),
    parent_github_issue_number INTEGER NOT NULL CHECK (parent_github_issue_number > 0),
    title TEXT NOT NULL,
    body_md TEXT NOT NULL,
    goal TEXT,
    acceptance_criteria_json TEXT NOT NULL,
    out_of_scope_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (
        status IN (
            'todo',
            'in_progress',
            'dev_in_progress',
            'qa_in_progress',
            'qa_failed',
            'review_in_progress',
            'review_failed',
            'pr_open',
            'ci_in_progress',
            'ci_failed',
            'ready_to_merge',
            'merge_in_progress',
            'merge_failed',
            'awaiting_human',
            'done',
            'blocked',
            'escalated',
            'canceled'
        )
    ),
    attempt_total INTEGER NOT NULL DEFAULT 0 CHECK (attempt_total >= 0),
    qa_fail_count INTEGER NOT NULL DEFAULT 0 CHECK (qa_fail_count >= 0),
    review_fail_count INTEGER NOT NULL DEFAULT 0 CHECK (review_fail_count >= 0),
    ci_fail_count INTEGER NOT NULL DEFAULT 0 CHECK (ci_fail_count >= 0),
    current_failure_fingerprint TEXT,
    active_pr_number INTEGER,
    task_branch TEXT,
    worktree_path TEXT,
    needs_human INTEGER NOT NULL DEFAULT 0 CHECK (needs_human IN (0, 1)),
    human_reason TEXT,
    opened_at TEXT,
    closed_at TEXT,
    last_issue_sync_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (sprint_id, task_local_id),
    UNIQUE (sprint_id, sequence_no),
    FOREIGN KEY (sprint_id) REFERENCES sprints (sprint_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    FOREIGN KEY (sprint_id, parent_github_issue_number) REFERENCES sprints (sprint_id, github_issue_number)
        ON UPDATE CASCADE
        ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id TEXT NOT NULL,
    depends_on_task_id TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source = 'github_issue_dependency'),
    created_at TEXT NOT NULL,
    PRIMARY KEY (task_id, depends_on_task_id),
    FOREIGN KEY (task_id) REFERENCES tasks (task_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    FOREIGN KEY (depends_on_task_id) REFERENCES tasks (task_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    CHECK (task_id <> depends_on_task_id)
);

CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY CHECK (length(trim(event_id)) > 0),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('task', 'sprint', 'system')),
    entity_id TEXT NOT NULL CHECK (length(trim(entity_id)) > 0),
    sprint_id TEXT,
    task_id TEXT,
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) > 0),
    source TEXT NOT NULL CHECK (length(trim(source)) > 0),
    attempt INTEGER CHECK (attempt IS NULL OR attempt >= 0),
    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(trim(idempotency_key)) > 0),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS review_findings (
    finding_id TEXT PRIMARY KEY CHECK (length(trim(finding_id)) > 0),
    task_id TEXT NOT NULL,
    review_event_id TEXT NOT NULL,
    reviewer_id TEXT NOT NULL CHECK (length(trim(reviewer_id)) > 0),
    lens TEXT NOT NULL CHECK (lens IN ('correctness', 'test', 'architecture', 'security')),
    severity TEXT NOT NULL CHECK (length(trim(severity)) > 0),
    confidence TEXT NOT NULL CHECK (length(trim(confidence)) > 0),
    category TEXT NOT NULL CHECK (length(trim(category)) > 0),
    file_refs_json TEXT NOT NULL,
    summary TEXT NOT NULL,
    evidence TEXT NOT NULL,
    finding_fingerprint TEXT NOT NULL CHECK (length(trim(finding_fingerprint)) > 0),
    suggested_action TEXT NOT NULL,
    aggregate_status TEXT NOT NULL DEFAULT 'open' CHECK (
        aggregate_status IN ('open', 'accepted', 'dismissed', 'fixed')
    ),
    created_at TEXT NOT NULL,
    UNIQUE (task_id, review_event_id, reviewer_id, finding_fingerprint),
    FOREIGN KEY (task_id) REFERENCES tasks (task_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    FOREIGN KEY (review_event_id) REFERENCES events (event_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS pull_requests (
    github_pr_number INTEGER PRIMARY KEY CHECK (github_pr_number > 0),
    github_pr_node_id TEXT NOT NULL UNIQUE CHECK (length(trim(github_pr_node_id)) > 0),
    pr_kind TEXT NOT NULL CHECK (pr_kind IN ('task', 'sprint')),
    sprint_id TEXT NOT NULL,
    task_id TEXT,
    head_branch TEXT NOT NULL CHECK (length(trim(head_branch)) > 0),
    base_branch TEXT NOT NULL CHECK (length(trim(base_branch)) > 0),
    status TEXT NOT NULL CHECK (status IN ('open', 'closed', 'merged')),
    auto_merge_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_merge_enabled IN (0, 1)),
    head_sha TEXT,
    url TEXT NOT NULL CHECK (length(trim(url)) > 0),
    opened_at TEXT,
    closed_at TEXT,
    merged_at TEXT,
    last_synced_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (sprint_id) REFERENCES sprints (sprint_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    FOREIGN KEY (task_id) REFERENCES tasks (task_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    CHECK (
        (pr_kind = 'task' AND task_id IS NOT NULL) OR
        (pr_kind = 'sprint' AND task_id IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS ci_runs (
    github_run_id INTEGER PRIMARY KEY CHECK (github_run_id > 0),
    sprint_id TEXT NOT NULL,
    task_id TEXT,
    github_pr_number INTEGER,
    workflow_name TEXT,
    head_sha TEXT,
    status TEXT NOT NULL CHECK (length(trim(status)) > 0),
    conclusion TEXT,
    html_url TEXT NOT NULL CHECK (length(trim(html_url)) > 0),
    started_at TEXT,
    completed_at TEXT,
    last_synced_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (sprint_id) REFERENCES sprints (sprint_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    FOREIGN KEY (task_id) REFERENCES tasks (task_id)
        ON UPDATE CASCADE
        ON DELETE RESTRICT,
    FOREIGN KEY (github_pr_number) REFERENCES pull_requests (github_pr_number)
        ON UPDATE CASCADE
        ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS outbox_actions (
    action_id TEXT PRIMARY KEY CHECK (length(trim(action_id)) > 0),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('task', 'sprint', 'system')),
    entity_id TEXT NOT NULL CHECK (length(trim(entity_id)) > 0),
    action_type TEXT NOT NULL CHECK (length(trim(action_type)) > 0),
    github_target_type TEXT NOT NULL CHECK (github_target_type IN ('issue', 'pull_request', 'label')),
    github_target_number INTEGER,
    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(trim(idempotency_key)) > 0),
    request_payload_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'canceled')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_error TEXT,
    next_attempt_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS sync_state (
    name TEXT PRIMARY KEY CHECK (length(trim(name)) > 0),
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status_sprint_sequence
    ON tasks (status, sprint_id, sequence_no);

CREATE INDEX IF NOT EXISTS idx_sprints_status_sequence
    ON sprints (status, sequence_no);

CREATE INDEX IF NOT EXISTS idx_events_entity_occurred_at
    ON events (entity_type, entity_id, occurred_at);

CREATE INDEX IF NOT EXISTS idx_events_event_type_occurred_at
    ON events (event_type, occurred_at);

CREATE INDEX IF NOT EXISTS idx_review_findings_task_fingerprint
    ON review_findings (task_id, finding_fingerprint);

CREATE INDEX IF NOT EXISTS idx_pull_requests_task_status
    ON pull_requests (task_id, status);

CREATE INDEX IF NOT EXISTS idx_ci_runs_pr_status
    ON ci_runs (github_pr_number, status);

CREATE INDEX IF NOT EXISTS idx_outbox_actions_status_next_attempt
    ON outbox_actions (status, next_attempt_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_pull_requests_one_open_task_pr
    ON pull_requests (task_id)
    WHERE pr_kind = 'task' AND status = 'open';

CREATE UNIQUE INDEX IF NOT EXISTS idx_pull_requests_one_open_sprint_pr
    ON pull_requests (sprint_id)
    WHERE pr_kind = 'sprint' AND status = 'open';

CREATE TRIGGER IF NOT EXISTS trg_task_dependencies_same_sprint_insert
BEFORE INSERT ON task_dependencies
FOR EACH ROW
BEGIN
    SELECT
        CASE
            WHEN (
                SELECT sprint_id
                FROM tasks
                WHERE task_id = NEW.task_id
            ) <> (
                SELECT sprint_id
                FROM tasks
                WHERE task_id = NEW.depends_on_task_id
            )
            THEN RAISE(ABORT, 'cross-sprint task dependency is not allowed')
        END;
END;

CREATE TRIGGER IF NOT EXISTS trg_task_dependencies_same_sprint_update
BEFORE UPDATE ON task_dependencies
FOR EACH ROW
BEGIN
    SELECT
        CASE
            WHEN (
                SELECT sprint_id
                FROM tasks
                WHERE task_id = NEW.task_id
            ) <> (
                SELECT sprint_id
                FROM tasks
                WHERE task_id = NEW.depends_on_task_id
            )
            THEN RAISE(ABORT, 'cross-sprint task dependency is not allowed')
        END;
END;

CREATE TRIGGER IF NOT EXISTS trg_events_no_update
BEFORE UPDATE ON events
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'events is append-only');
END;

CREATE TRIGGER IF NOT EXISTS trg_events_no_delete
BEFORE DELETE ON events
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'events is append-only');
END;

COMMIT;
