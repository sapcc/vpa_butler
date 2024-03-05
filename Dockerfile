FROM golang:1.22 as builder

ENV GOTOOLCHAIN=local
WORKDIR /workspace
COPY . .
ARG VERSION
RUN GO_LDFLAGS="main.Version=${VERSION}" make all

FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/sapcc/vpa_butler"
WORKDIR /
COPY --from=builder /workspace/bin/linux/vpa_butler .
USER nonroot:nonroot
ENTRYPOINT ["/vpa_butler"]
