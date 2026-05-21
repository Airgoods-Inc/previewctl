-- +goose Up
CREATE TABLE environment_proxy_activity (
    project        TEXT NOT NULL,
    name           TEXT NOT NULL,
    last_access_at TIMESTAMPTZ NOT NULL,
    last_host      TEXT,
    last_status    INTEGER,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project, name)
);

CREATE TABLE environment_cli_activity (
    project        TEXT NOT NULL,
    name           TEXT NOT NULL,
    last_access_at TIMESTAMPTZ NOT NULL,
    last_command   TEXT,
    last_actor     TEXT,
    last_machine   TEXT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project, name)
);

-- +goose Down
DROP TABLE IF EXISTS environment_cli_activity;
DROP TABLE IF EXISTS environment_proxy_activity;
