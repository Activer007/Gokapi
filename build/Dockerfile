FROM golang:1.26.10-bookworm

## To compile:
## cd SupaShare/build/
## docker build . --tag supashare-builder
## docker run --rm -it -v ../:/src/supashare -w /src/supashare supashare-builder

RUN \
  apt-get update && \
  apt-get install -y ca-certificates openssl zip && \
  update-ca-certificates && \
  rm -rf /var/lib/apt

COPY go.mod /tmp/tmp/go.mod

RUN cd /tmp/tmp/ && go mod download && rm -r /tmp/tmp

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
