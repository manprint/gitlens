# Lab image, no base image pulled: ha/build.sh compiles a static binary
# (CGO_ENABLED=0) and an empty state/ directory into the build context.
FROM scratch
COPY hactl /hactl
COPY --chown=65534:65534 state /state
USER 65534:65534
EXPOSE 5432 5433 8008
ENTRYPOINT ["/hactl"]
