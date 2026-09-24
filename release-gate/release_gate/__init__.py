"""Release Gate core logic.

Pure, dependency-light functions shared by the local runner and the Databricks
Spark jobs. Everything here is dict-in / dict-out so it can be unit-tested
without Spark or a live Databricks workspace.
"""

__version__ = "0.1.0"

# Evidence-bundle schema version this code targets (additive-compatible).
SUPPORTED_SCHEMA_MAJOR = 1
