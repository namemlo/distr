ARG NODE_IMAGE=node:26.5.0-alpine3.22
FROM ${NODE_IMAGE}

RUN apk add --no-cache ca-certificates openssh-client \
    && addgroup -g 10001 observer \
    && adduser -D -H -u 10001 -G observer observer \
    && install -d -o observer -g observer -m 0700 \
      /var/lib/choice-tp-observer/evidence \
      /var/lib/choice-tp-observer/intents \
      /var/lib/choice-tp-observer/state

WORKDIR /opt/choice-tp-observer
COPY --chown=observer:observer observer.mjs service.mjs ./

USER observer:observer
ENTRYPOINT ["node", "/opt/choice-tp-observer/service.mjs"]
CMD ["--config", "/etc/choice-tp-observer/service.json"]
