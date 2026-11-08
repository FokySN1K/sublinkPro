# syntax=docker/dockerfile:1

FROM metacubex/mihomo:latest AS mihomo

FROM mcr.microsoft.com/playwright/python:v1.55.0-noble

COPY --from=mihomo /mihomo /usr/local/bin/mihomo
RUN chmod +x /usr/local/bin/mihomo

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY tests ./tests

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    STATE_DIR=/data \
    MIHOMO_BIN=/usr/local/bin/mihomo

VOLUME ["/data"]

# Chromium runs headful inside a virtual display: YouTube serves ads (and bot
# checks) differently to an obviously headless browser.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["python", "-m", "app.main"]
