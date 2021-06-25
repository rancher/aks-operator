FROM registry.suse.com/suse/sle15:15.3
RUN zypper update -y && \
    zypper -n clean -a && \
    rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*
RUN useradd --uid 1007 aks-operator
ENV KUBECONFIG /home/aks-operator/.kube/config
COPY bin/aks-operator /usr/bin/
USER 1007
ENTRYPOINT ["aks-operator"]
