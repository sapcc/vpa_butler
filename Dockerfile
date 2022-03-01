FROM keppel.eu-de-2.cloud.sap/ccloud-dockerhub-mirror/library/golang:1.17 as builder

WORKDIR /workspace
COPY . .
RUN CGO_ENABLED=0 go build -o vpa_butler cmd/vpa_butler/main.go

FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/sapcc/vpa_butler"
WORKDIR /
COPY --from=builder /workspace/vpa_butler .
USER nonroot:nonroot
ENTRYPOINT ["/vpa_butler"]