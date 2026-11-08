"""Tests for the yt-ad-checker package.

Run them inside the image (the host may not have the dependencies)::

    docker build -t yt-ad-checker .
    docker run --rm --shm-size 512m --entrypoint python yt-ad-checker \
        -m unittest discover -s tests -t .
"""
