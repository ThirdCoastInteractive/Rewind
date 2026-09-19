FROM postgres:17-alpine
ARG PGVECTOR_VERSION=0.8.2
RUN apk add --no-cache --virtual .vector-build build-base git ca-certificates && \
    git clone --depth 1 --branch v${PGVECTOR_VERSION} https://github.com/pgvector/pgvector.git /tmp/pgvector && \
    make -C /tmp/pgvector OPTFLAGS="" with_llvm=no && make -C /tmp/pgvector install with_llvm=no && \
    rm -rf /tmp/pgvector && apk del .vector-build
# initdb copies this sample into PGDATA; existing clusters need a restart with the same setting.
RUN echo "shared_preload_libraries = 'pg_stat_statements'" >> "$(pg_config --sharedir)/postgresql.conf.sample"
