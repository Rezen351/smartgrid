CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS telemetry (
    time TIMESTAMPTZ NOT NULL,
    node_id VARCHAR(64) NOT NULL,
    module_id VARCHAR(64),
    metric VARCHAR(64) NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    raw JSONB DEFAULT '{}'
);

SELECT create_hypertable('telemetry', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_telemetry_node_metric_time ON telemetry (node_id, metric, time DESC);
CREATE INDEX IF NOT EXISTS idx_telemetry_time_node_metric ON telemetry (time ASC, node_id ASC, metric ASC);
