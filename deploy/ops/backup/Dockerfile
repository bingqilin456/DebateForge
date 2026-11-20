# T16A 工具镜像：复用 MySQL 官方量级的运行时，并从源码固定构建
# filippo.io/age v1.2.1（成熟、可审查的认证加密实现），不自造加密格式。
# 镜像内不含任何密钥；recipient 由调用方以文件/参数提供。
FROM golang:1.27.1 AS age-build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
RUN GOBIN=/out go install filippo.io/age/cmd/age@v1.2.1 filippo.io/age/cmd/age-keygen@v1.2.1

FROM mysql:8.4.13
COPY --from=age-build /out/age /usr/local/bin/age
COPY --from=age-build /out/age-keygen /usr/local/bin/age-keygen
COPY bin /opt/t16a/bin
COPY sql /opt/t16a/sql
COPY tests /opt/t16a/tests
WORKDIR /opt/t16a
ENTRYPOINT []
CMD ["bash", "/opt/t16a/tests/age-roundtrip.sh"]
