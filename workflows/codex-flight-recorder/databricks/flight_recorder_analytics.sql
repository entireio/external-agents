-- Codebase Flight Recorder — Databricks Free Edition notebook.
-- Create a SQL notebook, paste this file, and run cells in order.
-- Ingest only non-sensitive development telemetry. Do not upload prompts,
-- source code, credentials, or personal data.

CREATE SCHEMA IF NOT EXISTS codebase_flight_recorder;
USE SCHEMA codebase_flight_recorder;

CREATE TABLE IF NOT EXISTS development_history (
  session_id STRING,
  recorded_at TIMESTAMP,
  files_touched ARRAY<STRING>,
  tests ARRAY<STRING>,
  test_result STRING,
  retries INT,
  revert_count INT,
  summary STRING
) USING DELTA;

-- Load reviewed JSON records through the Databricks UI, then append them here.
-- The JSON must use the schema above; do not insert the synthetic demo fixture
-- when claiming production history.

CREATE OR REPLACE VIEW component_risk AS
WITH touched_files AS (
  SELECT
    file_path,
    session_id,
    test_result,
    COALESCE(retries, 0) AS retries,
    COALESCE(revert_count, 0) AS revert_count
  FROM development_history
  LATERAL VIEW explode(files_touched) exploded AS file_path
),
aggregated AS (
  SELECT
    file_path,
    COUNT(*) AS sessions,
    SUM(CASE WHEN lower(test_result) = 'failed' THEN 1 ELSE 0 END) AS failed_sessions,
    SUM(retries) AS retries,
    SUM(revert_count) AS reverts
  FROM touched_files
  GROUP BY file_path
)
SELECT
  *,
  LEAST(1.0, (failed_sessions * 0.40) + (retries * 0.08) + (reverts * 0.20)) AS risk_score
FROM aggregated;

-- This query produces the reviewed JSON fields consumed by
-- codex-flight-recorder --history. Download the result as JSON/CSV and retain
-- a visible provenance label when invoking the local workflow.
WITH session_files AS (
  SELECT
    h.session_id,
    h.recorded_at,
    h.files_touched,
    h.tests,
    h.test_result,
    h.retries,
    h.revert_count,
    h.summary,
    file_path
  FROM development_history h
  LATERAL VIEW explode(h.files_touched) expanded AS file_path
)
SELECT
  session_id,
  files_touched,
  tests,
  test_result,
  retries,
  revert_count,
  MAX(r.risk_score) AS risk_score,
  MAX(summary) AS summary
FROM session_files s
LEFT JOIN component_risk r ON s.file_path = r.file_path
GROUP BY session_id, files_touched, tests, test_result, retries, revert_count;
