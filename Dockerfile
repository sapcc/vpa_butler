FROM golang:1.23 as builder

ENV GOTOOLCHAIN=local
WORKDIR /workspace
COPY . .
ARG VERSION
RUN CGO_ENABLED=0 GO_LDFLAGS="main.Version=${VERSION}" make build-all

FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/sapcc/vpa_butler"
WORKDIR /
COPY --from=builder /workspace/build/vpa_butler /vpa_butler
USER nonroot:nonroot
ENTRYPOINT ["/vpa_butler"]
