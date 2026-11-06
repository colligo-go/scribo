package contract

const Version = 1

const ToolRunsDDL = `CREATE TABLE IF NOT EXISTS tool_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    contract_version INTEGER NOT NULL,
    tool             TEXT NOT NULL CHECK (tool IN ('colligo','scribo')),
    tool_version     TEXT NOT NULL,
    git_sha          TEXT NOT NULL,
    go_version       TEXT NOT NULL,
    host             TEXT,
    step             TEXT,
    config_hash      TEXT,
    argv_shape       TEXT NOT NULL,
    started_at       TEXT NOT NULL,
    ended_at         TEXT,
    exit_code        INTEGER
) STRICT;`
