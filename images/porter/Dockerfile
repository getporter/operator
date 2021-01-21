FROM carolynvs/porter:dev

# This is where files that need to be copied into /root/.porter/ should be mounted
VOLUME /porter-config

COPY run.sh /

ENTRYPOINT ["/run.sh"]