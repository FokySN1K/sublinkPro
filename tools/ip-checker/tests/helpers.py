"""Shared fakes for the ip-checker tests."""

from __future__ import annotations

import os
from typing import Mapping, Optional
from unittest import mock

import requests

from app.config import Settings


class FakeResponse:
    """The minimal ``requests.Response`` surface the sources touch."""

    def __init__(self, status: int = 200, body: str = "") -> None:
        self.status_code = status
        self.text = body


class FakeSession:
    """A session that answers from a URL -> response mapping.

    A missing URL raises ``ConnectionError``, which is how a dead endpoint looks
    to the code under test.
    """

    def __init__(self, answers: Optional[Mapping[str, FakeResponse]] = None) -> None:
        self.answers = dict(answers or {})
        self.calls: list[str] = []
        self.headers: dict[str, str] = {}
        self.proxies: dict[str, str] = {}
        self.trust_env = True

    def get(self, url: str, **_kwargs: object) -> FakeResponse:
        self.calls.append(url)
        if url not in self.answers:
            raise requests.ConnectionError(f"no route to {url}")
        return self.answers[url]

    def close(self) -> None:
        return None


def make_settings(**env: object) -> Settings:
    """Build Settings from a throwaway environment, one test per case."""
    values = {"SUBLINK_API_KEY": "test-key"}
    values.update({key: str(value) for key, value in env.items()})
    with mock.patch.dict(os.environ, values, clear=True):
        return Settings.from_env()
