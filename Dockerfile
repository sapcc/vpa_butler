FROM keppel.eu-de-2.cloud.sap/ccloud-dockerhub-mirror/library/golang:1.19 as builder

WORKDIR /workspace
COPY . .
RUN make all

FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/sapcc/vpa_butler"
WORKDIR /
COPY --from=builder /workspace/bin/linux/vpa_butler .
USER nonroot:nonroot
ENTRYPOINT ["/vpa_butler"]
