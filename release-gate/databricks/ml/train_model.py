"""Train the Release Gate risk model with MLflow tracking + Model Registry.

Runs on Databricks (serverless) or locally. Prefers a gradient-boosted model
(LightGBM if present, else scikit-learn's GradientBoostingClassifier -- both
CPU-friendly, within Free Edition limits: no GPU/provisioned throughput).

Locally:
    python databricks/ml/train_model.py --data seed_data/pr_history.jsonl
On Databricks: run as a job task; set ``register`` to push to the Model Registry.
"""
from __future__ import annotations

import argparse
import json
import os
import sys

_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)

from release_gate.model import FEATURE_COLUMNS, LABEL_COLUMN  # noqa: E402

REGISTERED_MODEL_NAME = "release_gate_risk"

try:
    from mlflow.pyfunc import PythonModel as _PythonModel
except Exception:  # noqa: BLE001 - mlflow only needed at train time
    _PythonModel = object


class RiskProbaModel(_PythonModel):
    """Serve a graded P(incident) in [0,1] instead of a hard 0/1 class."""

    def __init__(self, model=None):
        self._model = model

    def predict(self, context, model_input, params=None):
        X = getattr(model_input, "values", model_input)
        return [float(p[1]) for p in self._model.predict_proba(X)]


def _load(data_path: str):
    X, y = [], []
    with open(data_path, "r", encoding="utf-8") as fh:
        for line in fh:
            row = json.loads(line)
            X.append([float(row.get(c, 0) or 0) for c in FEATURE_COLUMNS])
            y.append(int(row[LABEL_COLUMN]))
    return X, y


def _build_model():
    try:
        from lightgbm import LGBMClassifier  # type: ignore

        return LGBMClassifier(n_estimators=200, learning_rate=0.05, max_depth=4,
                              subsample=0.9, random_state=42), "lightgbm"
    except ImportError:
        from sklearn.ensemble import GradientBoostingClassifier

        return GradientBoostingClassifier(
            n_estimators=200, learning_rate=0.05, max_depth=3, random_state=42
        ), "sklearn-gbt"


def train(data_path: str, register: bool) -> dict:
    import mlflow
    import mlflow.sklearn
    from sklearn.metrics import accuracy_score, f1_score, roc_auc_score
    from sklearn.model_selection import cross_val_score, train_test_split

    X, y = _load(data_path)
    Xtr, Xte, ytr, yte = train_test_split(
        X, y, test_size=0.25, random_state=42, stratify=y
    )
    model, flavor = _build_model()

    experiment = os.environ.get("RG_EXPERIMENT")
    if not experiment:
        experiment = ("/Shared/release_gate_risk" if _on_databricks()
                      else "release_gate_risk")
    mlflow.set_experiment(experiment)

    # Unity Catalog model registry when requested (3-level name required).
    registry_uri = os.environ.get("MLFLOW_REGISTRY_URI")
    if registry_uri:
        mlflow.set_registry_uri(registry_uri)
    model_name = os.environ.get("RG_MODEL_NAME", REGISTERED_MODEL_NAME)
    with mlflow.start_run() as run:
        # Cross-validated AUC on the full set for a stable estimate.
        cv_model, _ = _build_model()
        cv_auc = float(cross_val_score(cv_model, X, y, cv=5, scoring="roc_auc").mean())

        model.fit(Xtr, ytr)
        proba = [p[1] for p in model.predict_proba(Xte)]
        preds = [1 if p >= 0.5 else 0 for p in proba]

        metrics = {
            "cv_auc": cv_auc,
            "holdout_auc": float(roc_auc_score(yte, proba)) if len(set(yte)) > 1 else 0.0,
            "accuracy": float(accuracy_score(yte, preds)),
            "f1": float(f1_score(yte, preds, zero_division=0)),
        }
        mlflow.log_params({
            "flavor": flavor, "n_train": len(Xtr), "n_test": len(Xte),
            "features": len(FEATURE_COLUMNS),
        })
        mlflow.log_metrics(metrics)

        signature = None
        input_example = None
        try:
            import pandas as pd
            from mlflow.models.signature import infer_signature
            Xte_df = pd.DataFrame(Xte, columns=FEATURE_COLUMNS)
            signature = infer_signature(Xte_df, proba)
            input_example = Xte_df.head(1)
        except Exception:  # noqa: BLE001
            pass

        kwargs = {"signature": signature}
        if input_example is not None:
            kwargs["input_example"] = input_example
        if register:
            kwargs["registered_model_name"] = model_name
        # Serve a graded probability (risk_score in [0,1]) via a pyfunc wrapper.
        import mlflow.pyfunc
        mlflow.pyfunc.log_model(
            artifact_path="model", python_model=RiskProbaModel(model),
            pip_requirements=["scikit-learn", "mlflow", "pandas"], **kwargs)

        result = {"run_id": run.info.run_id, "flavor": flavor, **metrics}
        print("[train_model] " + json.dumps(result))
        return result


def _on_databricks() -> bool:
    return "DATABRICKS_RUNTIME_VERSION" in os.environ


def main(argv=None) -> int:
    p = argparse.ArgumentParser(description="Train the Release Gate risk model.")
    p.add_argument("--data", default=os.path.join(_ROOT, "seed_data", "pr_history.jsonl"))
    p.add_argument("--register", action="store_true")
    args = p.parse_args(argv)
    train(args.data, args.register)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
