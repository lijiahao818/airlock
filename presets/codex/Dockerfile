FROM node:24.22.1-bookworm-slim@sha256:55fb5f5a527ea2c5c4a95f963e48ef90aa38c7b346054fdc8ee9957a2b55d205
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git tmux \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @openai/codex@0.164.0 \
 && npm cache clean --force
USER node
WORKDIR /workspace
