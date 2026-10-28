-- Release Gate — Lakeview / SQL dashboard queries (fallback for the Databricks App).
-- Paste each block as a visualization on a Databricks SQL dashboard bound to the
-- single serverless 2X-Small warehouse. Catalog defaults to `release_gate`.

-- 1) Latest PR risk scores (table / counter).
SELECT pr_number, revision_sha, risk_score, gate, model, scored_at
FROM release_gate.gold.pr_risk_scores
ORDER BY scored_at DESC
LIMIT 50;

-- 2) Gate distribution (bar/pie).
SELECT gate, COUNT(*) AS prs
FROM release_gate.gold.pr_risk_scores
GROUP BY gate;

-- 3) Risk vs. blast radius — shows the Entire Graph signal driving risk (scatter).
SELECT f.pr_number,
       f.blast_radius_symbols,
       f.unresolved_risk_count,
       s.risk_score,
       s.gate
FROM release_gate.gold.pr_risk_features f
JOIN release_gate.gold.pr_risk_scores s
  ON f.pr_number = s.pr_number AND f.revision_sha = s.revision_sha;

-- 4) Evidence drill-down for one PR (table). Parameterize :pr_number.
SELECT *
FROM release_gate.gold.pr_risk_features
WHERE pr_number = :pr_number;

-- 5) Unresolved-risk leaderboard — the Entire Checkpoints signal (bar).
SELECT pr_number, unresolved_risk_count, checkpoint_count
FROM release_gate.gold.pr_risk_features
ORDER BY unresolved_risk_count DESC
LIMIT 20;

-- 6) Before/after Curveball — compare model versions on the same PR (table).
SELECT pr_number, model, risk_score, gate, scored_at
FROM release_gate.gold.pr_risk_scores
ORDER BY pr_number, scored_at;
