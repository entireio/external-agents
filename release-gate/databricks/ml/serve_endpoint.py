"""Deploy the Release Gate risk model to a Databricks Model Serving endpoint.

Serverless, scale-to-zero (Free Edition friendly: no GPU/provisioned
throughput). Idempotent: updates the endpoint config if it already exists.

    python databricks/ml/serve_endpoint.py --profile release-gate \
        --model release_gate.gold.risk_model --version 1
"""
from __future__ import annotations

import argparse

ENDPOINT = "release-gate-risk"


def deploy(profile: str, model: str, version: str) -> None:
    from databricks.sdk import WorkspaceClient
    from databricks.sdk.service.serving import (EndpointCoreConfigInput,
                                                ServedEntityInput)

    w = WorkspaceClient(profile=profile)
    served = [ServedEntityInput(
        entity_name=model, entity_version=version,
        scale_to_zero_enabled=True, workload_size="Small",
    )]
    existing = [e.name for e in w.serving_endpoints.list()]
    if ENDPOINT in existing:
        print(f"[serve] updating endpoint {ENDPOINT}")
        w.serving_endpoints.update_config_and_wait(
            name=ENDPOINT, served_entities=served)
    else:
        print(f"[serve] creating endpoint {ENDPOINT}")
        w.serving_endpoints.create_and_wait(
            name=ENDPOINT,
            config=EndpointCoreConfigInput(name=ENDPOINT, served_entities=served),
        )
    ep = w.serving_endpoints.get(ENDPOINT)
    print(f"[serve] endpoint {ENDPOINT} state={ep.state}")


def main(argv=None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--profile", default="release-gate")
    p.add_argument("--model", default="release_gate.gold.risk_model")
    p.add_argument("--version", default="1")
    args = p.parse_args(argv)
    deploy(args.profile, args.model, args.version)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
