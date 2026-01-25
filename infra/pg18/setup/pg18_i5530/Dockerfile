FROM postgres:18-alpine

RUN set -eux; \
	sed -ri "s!^#?(port)\s*=\s*\S+!\1 = 5530!" /usr/local/share/postgresql/postgresql.conf.sample; \
	grep -ri "^port = 5530" /usr/local/share/postgresql/postgresql.conf.sample

RUN apk add --no-cache pg_cron

RUN cp /usr/lib/postgresql18/pg_cron.so /usr/local/lib/postgresql/ && \
    cp /usr/share/postgresql18/extension/pg_cron* /usr/local/share/postgresql/extension/

EXPOSE 5530

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD /HEALTHCHECK.sh

CMD ["postgres", "-p", "5530"]