# syntax=docker/dockerfile:1

FROM metacubex/mihomo:latest AS mihomo

# HTTP probes only: no Chromium, no virtual display, so the image stays small
# and there is no Xvfb to clean up between restarts.
FROM python:3.12-slim

COPY --from=mihomo /mihomo /usr/local/bin/mihomo
RUN chmod +x /usr/local/bin/mihomo

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY tests ./tests

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    STATE_DIR=/data \
    MIHOMO_BIN=/usr/local/bin/mihomo

VOLUME ["/data"]

# Python receives SIGTERM directly and finishes the node in flight.
CMD ["python", "-m", "app.main"]
