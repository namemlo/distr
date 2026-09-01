ARG NODE_IMAGE=node:24.8.0-alpine3.22@sha256:3e843c608bb5232f39ecb2b25e41214b958b0795914707374c8acc28487dea17
FROM ${NODE_IMAGE}

ARG NODE_IMAGE
ARG SOURCE_REVISION
RUN printf '%s\n' "$NODE_IMAGE" | grep -Eq '@sha256:[0-9a-f]{64}$' \
    && printf '%s\n' "$SOURCE_REVISION" | grep -Eq '^[0-9a-f]{40}$' \
    && apk add --no-cache ca-certificates openssh-client \
    && addgroup -g 10001 observer \
    && adduser -D -H -u 10001 -G observer observer \
    && install -d -o observer -g observer -m 0700 \
      /var/lib/choice-tp-observer/evidence \
      /var/lib/choice-tp-observer/intents \
      /var/lib/choice-tp-observer/state

LABEL org.opencontainers.image.source="https://github.com/namemlo/distr" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.title="Choice TP independent observer"

WORKDIR /opt/choice-tp-observer
COPY --chown=observer:observer observer.mjs service.mjs ./

USER observer:observer
ENTRYPOINT ["node", "/opt/choice-tp-observer/service.mjs"]
CMD ["--config", "/etc/choice-tp-observer/service.json"]
