# 证书容器。官方 certbot 镜像 + 本项目的签发/续期循环。
# 单独做成镜像（而不是挂脚本进官方镜像）是为了让服务器上只需要 compose.yaml 和 .env，
# 不需要仓库里的脚本文件。
FROM certbot/certbot:v5.1.0

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
