FROM registry.suse.com/bci/bci-base:15.6 AS base
RUN sed -i 's/^CREATE_MAIL_SPOOL=yes/CREATE_MAIL_SPOOL=no/' /etc/default/useradd
RUN useradd --uid 1007 aks-operator

FROM registry.suse.com/bci/golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY ./controller ./controller
COPY ./pkg ./pkg
COPY ./main.go ./main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /aks-operator

FROM registry.suse.com/bci/bci-micro:15.6
COPY --from=base /etc/passwd /etc/passwd
COPY --from=base /etc/shadow /etc/shadow
COPY --from=builder /aks-operator /usr/bin/aks-operator

RUN rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*

ENV KUBECONFIG="/home/aks-operator/.kube/config"
ENV SSL_CERT_DIR="/etc/rancher/ssl"

COPY package/entrypoint.sh /usr/bin
RUN chmod +x /usr/bin/entrypoint.sh

RUN mkdir -p /etc/rancher/ssl && \
    chown -R aks-operator /etc/rancher/ssl

USER 1007
ENTRYPOINT ["entrypoint.sh"]
