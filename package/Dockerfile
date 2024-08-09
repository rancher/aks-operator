FROM registry.suse.com/bci/bci-base:15.6 AS builder
RUN sed -i 's/^CREATE_MAIL_SPOOL=yes/CREATE_MAIL_SPOOL=no/' /etc/default/useradd
RUN useradd --uid 1007 aks-operator

FROM registry.suse.com/bci/bci-micro:15.6
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/shadow /etc/shadow

RUN rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*

ENV KUBECONFIG /home/aks-operator/.kube/config
ENV SSL_CERT_DIR /etc/rancher/ssl

COPY bin/aks-operator /usr/bin/
COPY package/entrypoint.sh /usr/bin
RUN chmod +x /usr/bin/entrypoint.sh

RUN mkdir -p /etc/rancher/ssl && \
    chown -R aks-operator /etc/rancher/ssl

USER 1007
ENTRYPOINT ["entrypoint.sh"]
