# PostgreSQL 18（本地开发与 Dokploy 生产共用），额外编译 pg_bigm（中文二元切分全文检索）。
# PGDG apt 源没有 pg_bigm 的 PG 18 包，所以从源码构建，构建工具不留在最终镜像里。
ARG PG_IMAGE=postgres:18.6-bookworm

FROM ${PG_IMAGE} AS build
ARG PG_BIGM_VERSION=v1.2-20250903
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl build-essential postgresql-server-dev-18; \
    mkdir -p /src; \
    curl -fsSL "https://github.com/pgbigm/pg_bigm/archive/refs/tags/${PG_BIGM_VERSION}.tar.gz" \
      | tar -xz -C /src --strip-components=1; \
    cd /src; \
    make USE_PGXS=1; \
    make USE_PGXS=1 install DESTDIR=/out

FROM ${PG_IMAGE}
COPY --from=build /out/usr/lib/postgresql/18/lib/ /usr/lib/postgresql/18/lib/
COPY --from=build /out/usr/share/postgresql/18/extension/ /usr/share/postgresql/18/extension/

# 官方 entrypoint 只在 PGDATA 为空时依次执行 /docker-entrypoint-initdb.d 下的脚本。
# 这一步创建 xsite、xsite_app、xsite_ro、常用扩展与权限；业务表由 service 的 migrate 服务负责。
# 本地 compose 即使再把同目录 bind mount 进来，也只是用宿主机同一份脚本覆盖，行为一致。
COPY init/01-init.sh /docker-entrypoint-initdb.d/01-init.sh
RUN chmod 0755 /docker-entrypoint-initdb.d/01-init.sh
