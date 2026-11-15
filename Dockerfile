# Build
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
# go mod tidy writes go.sum when the repository does not carry one yet.
RUN go mod tidy \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sammati ./cmd/sammati \
 && mkdir /out/data

# Run: no shell, no package manager, not root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sammati /sammati
COPY --from=build --chown=65532:65532 /out/data /data
ENV SAMMATI_DB=/data/sammati.db \
    SAMMATI_ADDR=0.0.0.0:8787
VOLUME /data
EXPOSE 8787
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/sammati", "health"]
ENTRYPOINT ["/sammati"]
CMD ["serve"]
