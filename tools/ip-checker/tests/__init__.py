"""Tests for the ip-checker package.

Run them inside the image (the host may not have the dependencies)::

    docker build -t ip-checker .
    docker run --rm --entrypoint python ip-checker -m unittest discover -s tests -t .
"""

from __future__ import annotations
