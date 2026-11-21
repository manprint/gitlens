# Local test image for the hypopg index check (plan 007 phase 10) and the
# tuning workbook hints (phase 13). Built by test/pgext/build.sh; never pushed.
FROM postgres:18.6-alpine
COPY src/hypopg.tgz src/hint.tgz /tmp/
RUN set -eux; \
	apk add --no-cache --virtual .build build-base flex bison; \
	mkdir -p /tmp/hypopg /tmp/hint; \
	tar -xzf /tmp/hypopg.tgz -C /tmp/hypopg --strip-components=1; \
	tar -xzf /tmp/hint.tgz -C /tmp/hint --strip-components=1; \
	make -C /tmp/hypopg USE_PGXS=1 with_llvm=no install; \
	make -C /tmp/hint USE_PGXS=1 with_llvm=no install || touch /pg_hint_plan.FAILED; \
	apk del .build; \
	rm -rf /tmp/*
