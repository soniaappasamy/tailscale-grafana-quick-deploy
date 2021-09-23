# Build our startup file.
FROM golang:1.16.2-alpine3.13 as go-builder
WORKDIR /app
COPY . ./
RUN go build -o app ./main.go

# Build tailscale & tailscaled.
FROM alpine:latest as tailscale
WORKDIR /app
ENV TSFILE=tailscale_1.14.0_amd64.tgz
RUN wget https://pkgs.tailscale.com/stable/${TSFILE} && \
  tar xzf ${TSFILE} --strip-components=1

# Final layer, built off grafana.
FROM grafana/grafana:latest
ENV GF_INSTALL_PLUGINS grafana-piechart-panel
ADD start.sh /
ADD grafana.ini /etc/grafana/grafana.ini

# Copy binaries to production image.
COPY --from=go-builder /app/app /app/app
COPY --from=tailscale /app/tailscaled /app/tailscaled
COPY --from=tailscale /app/tailscale /app/tailscale

# Run on container startup.
ENTRYPOINT [ "/start.sh" ]
