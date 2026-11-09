"""Tests for the ai-checker package.

Run them inside the image (the host may not have the dependencies)::

    docker build -t ai-checker .
    docker run --rm --entrypoint python ai-checker -m unittest discover -s tests -t .
"""

from __future__ import annotations
