# Runtime for @deepseek-ai/dsh@0.1.1-rc.2. Do not reuse this file for other versions.
FROM node:22-bookworm-slim

ARG DSH_VERSION=0.1.1-rc.2
RUN test "$DSH_VERSION" = "0.1.1-rc.2"

RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates python3 make g++ \
    && rm -rf /var/lib/apt/lists/*

RUN corepack enable

RUN npm install -g "@deepseek-ai/dsh@${DSH_VERSION}"

COPY 0.1.1-rc.2/patch-frontend.mjs /usr/local/bin/dsh-patch-frontend.mjs
RUN node /usr/local/bin/dsh-patch-frontend.mjs

LABEL dsh-testsuite.runtime="1" \
      dsh-testsuite.dsh-version="${DSH_VERSION}"

ENV DSH_HOME=/data/dsh \
    DSH_TELEMETRY_DISABLED=1 \
    npm_config_update_notifier=false

WORKDIR /workspace

COPY 0.1.1-rc.2/entrypoint.sh /usr/local/bin/dsh-entrypoint.sh
COPY common/proxy.py /usr/local/bin/dsh-proxy.py
COPY common/allow_builds.py /usr/local/bin/dsh-allow-builds.py
RUN chmod +x /usr/local/bin/dsh-entrypoint.sh /usr/local/bin/dsh-proxy.py /usr/local/bin/dsh-allow-builds.py

EXPOSE 3080

ENTRYPOINT ["/usr/local/bin/dsh-entrypoint.sh"]
