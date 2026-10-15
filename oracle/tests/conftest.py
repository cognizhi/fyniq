"""Hypothesis profiles for the oracle's own tests.

``oracle`` (the default, run by ``make oracle-test`` inside ``make test``) is derandomized, so the
same examples run every time and a red run reproduces. ``thorough`` runs many more; select it with
ORACLE_HYPOTHESIS_PROFILE=thorough.
"""

import os

from hypothesis import HealthCheck, settings

settings.register_profile(
    "oracle",
    max_examples=150,
    derandomize=True,
    database=None,
    deadline=None,
    suppress_health_check=[
        HealthCheck.too_slow,
        HealthCheck.data_too_large,
        HealthCheck.filter_too_much,
        HealthCheck.large_base_example,
    ],
)
settings.register_profile("thorough", parent=settings.get_profile("oracle"), max_examples=2000)
settings.load_profile(os.environ.get("ORACLE_HYPOTHESIS_PROFILE", "oracle"))
