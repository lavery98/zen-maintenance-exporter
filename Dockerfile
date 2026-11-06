FROM gcr.io/distroless/static-debian13:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/zen-maintenance-exporter /zen-maintenance-exporter
ENTRYPOINT ["/zen-maintenance-exporter"]
EXPOSE 10062
