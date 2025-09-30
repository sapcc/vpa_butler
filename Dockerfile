# SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
#
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.25 as builder

ENV GOTOOLCHAIN=local
WORKDIR /workspace
COPY . .
ARG VERSION
RUN CGO_ENABLED=0 GO_LDFLAGS="-X main.Version=${VERSION}" make build-all

FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/sapcc/vpa_butler"
WORKDIR /
COPY --from=builder /workspace/build/vpa_butler /vpa_butler
USER nonroot:nonroot
ENTRYPOINT ["/vpa_butler"]
